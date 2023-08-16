// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	smath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"

	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/consts"
	"github.com/ava-labs/hypersdk/emap"
	"github.com/ava-labs/hypersdk/mempool"
	"github.com/ava-labs/hypersdk/tstate"
	"github.com/ava-labs/hypersdk/utils"
)

var (
	_ emap.Item    = (*Transaction)(nil)
	_ mempool.Item = (*Transaction)(nil)
)

type Transaction struct {
	Base        *Base         `json:"base"`
	WarpMessage *warp.Message `json:"warpMessage"`
	Action      Action        `json:"action"`
	Auth        Auth          `json:"auth"`

	digest         []byte
	bytes          []byte
	size           int
	id             ids.ID
	numWarpSigners int
	// warpID is just the hash of the *warp.Message.Payload. We assumed that
	// all warp messages from a single source have some unique field that
	// prevents duplicates (like txID). We will not allow 2 instances of the same
	// warpID from the same sourceChainID to be accepted.
	warpID    ids.ID
	stateKeys [][]byte
}

type WarpResult struct {
	Message   *warp.Message
	VerifyErr error
}

func NewTx(base *Base, wm *warp.Message, act Action) *Transaction {
	return &Transaction{
		Base:        base,
		WarpMessage: wm,
		Action:      act,
	}
}

func (t *Transaction) Digest() ([]byte, error) {
	if len(t.digest) > 0 {
		return t.digest, nil
	}
	actionID := t.Action.GetTypeID()
	var warpBytes []byte
	if t.WarpMessage != nil {
		warpBytes = t.WarpMessage.Bytes()
	}
	size := t.Base.Size() +
		codec.BytesLen(warpBytes) +
		consts.ByteLen + t.Action.Size()
	p := codec.NewWriter(size, consts.NetworkSizeLimit)
	t.Base.Marshal(p)
	p.PackBytes(warpBytes)
	p.PackByte(actionID)
	t.Action.Marshal(p)
	return p.Bytes(), p.Err()
}

func (t *Transaction) Sign(
	factory AuthFactory,
	actionRegistry ActionRegistry,
	authRegistry AuthRegistry,
) (*Transaction, error) {
	// Generate auth
	msg, err := t.Digest()
	if err != nil {
		return nil, err
	}
	auth, err := factory.Sign(msg, t.Action)
	if err != nil {
		return nil, err
	}
	t.Auth = auth

	// Ensure transaction is fully initialized and correct by reloading it from
	// bytes
	size := len(msg) + consts.ByteLen + t.Auth.Size()
	p := codec.NewWriter(size, consts.NetworkSizeLimit)
	if err := t.Marshal(p); err != nil {
		return nil, err
	}
	if err := p.Err(); err != nil {
		return nil, err
	}
	p = codec.NewReader(p.Bytes(), consts.MaxInt)
	return UnmarshalTx(p, actionRegistry, authRegistry)
}

func (t *Transaction) AuthAsyncVerify() func() error {
	return func() error {
		return t.Auth.AsyncVerify(t.digest)
	}
}

func (t *Transaction) Bytes() []byte { return t.bytes }

func (t *Transaction) Size() int { return t.size }

func (t *Transaction) ID() ids.ID { return t.id }

func (t *Transaction) Expiry() int64 { return t.Base.Timestamp }

func (t *Transaction) MaxFee() uint64 { return t.Base.MaxFee }

// It is ok to have duplicate ReadKeys...the processor will skip them
func (t *Transaction) StateKeys(stateMapping StateManager) [][]byte {
	// We assume that any transaction must modify some state key (at least to pay
	// fees)
	if len(t.stateKeys) != 0 {
		return t.stateKeys
	}
	keys := append(t.Action.StateKeys(t.Auth, t.ID()), t.Auth.StateKeys()...)
	if t.WarpMessage != nil {
		keys = append(keys, stateMapping.IncomingWarpKey(t.WarpMessage.SourceChainID, t.warpID))
	}
	// Always assume a message could export a warp message
	keys = append(keys, stateMapping.OutgoingWarpKey(t.id))
	t.stateKeys = keys
	return keys
}

// Units is charged whether or not a transaction is successful because state
// lookup is not free.
func (t *Transaction) MaxUnits(r Rules) (Dimensions, error) {
	d, err := Add(t.Action.MaxUnits(r), t.Auth.MaxUnits(r))
	if err != nil {
		return Dimensions{}, err
	}
	if err := d.Add(Compute, r.GetBaseComputeUnits()); err != nil {
		return Dimensions{}, err
	}
	if t.WarpMessage != nil {
		if err := d.Add(Compute, r.GetBaseWarpComputeUnits()); err != nil {
			return Dimensions{}, err
		}
		warpSignerComputeUnits, err := smath.Mul64(uint64(t.numWarpSigners), r.GetWarpComputeUnitsPerSigner())
		if err != nil {
			return Dimensions{}, err
		}
		if err := d.Add(Compute, warpSignerComputeUnits); err != nil {
			return Dimensions{}, err
		}
	}
	return d, nil
}

// PreExecute must not modify state
func (t *Transaction) PreExecute(
	ctx context.Context,
	feeManager *FeeManager,
	r Rules,
	db Database,
	timestamp int64,
) error {
	if err := t.Base.Execute(r.ChainID(), r, timestamp); err != nil {
		return err
	}
	start, end := t.Action.ValidRange(r)
	if start >= 0 && timestamp < start {
		return ErrActionNotActivated
	}
	if end >= 0 && timestamp > end {
		return ErrActionNotActivated
	}
	start, end = t.Auth.ValidRange(r)
	if start >= 0 && timestamp < start {
		return ErrAuthNotActivated
	}
	if end >= 0 && timestamp > end {
		return ErrAuthNotActivated
	}
	if t.Base.MaxFee < r.GetMinFee() {
		return ErrInvalidFee
	}
	// TODO: check after fee
	if _, err := t.Auth.Verify(ctx, r, db, t.Action); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthFailed, err) //nolint:errorlint
	}
	maxUnits, err := t.MaxUnits(r)
	if err != nil {
		return err
	}
	maxFee, err := feeManager.MaxFee(maxUnits)
	if err != nil {
		return err
	}
	return t.Auth.CanDeduct(ctx, db, maxFee)
}

// Execute after knowing a transaction can pay a fee
func (t *Transaction) Execute(
	ctx context.Context,
	feeManager *FeeManager,
	r Rules,
	s StateManager,
	tdb *tstate.TState,
	timestamp int64,
	warpVerified bool,
) (*Result, error) {
	// Check warp message is not duplicate
	if t.WarpMessage != nil {
		_, err := tdb.GetValue(ctx, s.IncomingWarpKey(t.WarpMessage.SourceChainID, t.warpID))
		switch {
		case err == nil:
			// Override all errors if warp message is a duplicate
			//
			// TODO: consider doing this check before performing signature
			// verification.
			warpVerified = false
		case errors.Is(err, database.ErrNotFound):
			// This means there are no conflicts
		case err != nil:
			return nil, err
		}
	}

	// Verify auth is correct prior to doing anything
	//
	// TODO: run this after checking fee?
	authUsed, err := t.Auth.Verify(ctx, r, tdb, t.Action)
	if err != nil {
		return nil, err
	}

	// Always charge fee first in case [Action] moves funds
	maxUnits, err := t.MaxUnits(r)
	if err != nil {
		// Should never happen
		return nil, err
	}
	maxFee, err := feeManager.MaxFee(maxUnits)
	if err != nil {
		// Should never happen
		return nil, err
	}
	if err := t.Auth.Deduct(ctx, tdb, maxFee); err != nil {
		// This should never fail for low balance (as we check [CanDeductFee]
		// immediately before.
		return nil, err
	}

	// We create a temp state to ensure we don't commit failed actions to state.
	start := tdb.OpIndex()
	result, err := t.Action.Execute(ctx, r, tdb, timestamp, t.Auth, t.id, warpVerified)
	if err != nil {
		return nil, err
	}
	if len(result.Output) == 0 && result.Output != nil {
		// Enforce object standardization (this is a VM bug and we should fail
		// fast)
		return nil, ErrInvalidObject
	}
	if !result.Success {
		// Only keep changes if successful
		result.WarpMessage = nil // warp messages can only be emitted on success
		tdb.Rollback(ctx, start)
	}

	// Refund all units that went unused
	//
	// TODO: don't redo this logic here, create standard location
	// for warp
	used, err := Add(authUsed, result.Used)
	if err != nil {
		return nil, err
	}
	if err := used.Add(Compute, r.GetBaseComputeUnits()); err != nil {
		return nil, err
	}
	if t.WarpMessage != nil {
		if err := used.Add(Compute, r.GetBaseWarpComputeUnits()); err != nil {
			return nil, err
		}
		warpSignerComputeUnits, err := smath.Mul64(uint64(t.numWarpSigners), r.GetWarpComputeUnitsPerSigner())
		if err != nil {
			return nil, err
		}
		if err := used.Add(Compute, warpSignerComputeUnits); err != nil {
			return nil, err
		}
	}
	feeRequired, err := feeManager.MaxFee(used)
	if err != nil {
		return nil, err
	}

	// Return any funds from unused units
	refund := maxFee - feeRequired
	if refund > 0 {
		if err := t.Auth.Refund(ctx, tdb, refund); err != nil {
			return nil, err
		}
	}

	// Handle all warp updates (if the transaction was successful)
	if result.Success {
		// Store incoming warp messages in state by their ID to prevent replays
		if t.WarpMessage != nil {
			if err := tdb.Insert(ctx, s.IncomingWarpKey(t.WarpMessage.SourceChainID, t.warpID), nil); err != nil {
				return nil, err
			}
		}

		// Store newly created warp messages in state by their txID to ensure we can
		// always sign for a message
		if result.WarpMessage != nil {
			// Enforce we are the source of our own messages
			result.WarpMessage.NetworkID = r.NetworkID()
			result.WarpMessage.SourceChainID = r.ChainID()
			// Initialize message (compute bytes) now that everything is populated
			if err := result.WarpMessage.Initialize(); err != nil {
				return nil, err
			}
			// We use txID here because did not know the warpID before execution (and
			// we pre-reserve this key for the processor).
			if err := tdb.Insert(ctx, s.OutgoingWarpKey(t.id), result.WarpMessage.Bytes()); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// Used by mempool
func (t *Transaction) Payer() string {
	return string(t.Auth.Payer())
}

func (t *Transaction) Marshal(p *codec.Packer) error {
	if len(t.bytes) > 0 {
		p.PackFixedBytes(t.bytes)
		return p.Err()
	}

	actionID := t.Action.GetTypeID()
	authID := t.Auth.GetTypeID()
	t.Base.Marshal(p)
	var warpBytes []byte
	if t.WarpMessage != nil {
		warpBytes = t.WarpMessage.Bytes()
		if len(warpBytes) == 0 {
			return ErrWarpMessageNotInitialized
		}
	}
	p.PackBytes(warpBytes)
	p.PackByte(actionID)
	t.Action.Marshal(p)
	p.PackByte(authID)
	t.Auth.Marshal(p)
	return p.Err()
}

func MarshalTxs(txs []*Transaction) ([]byte, error) {
	if len(txs) == 0 {
		return nil, ErrNoTxs
	}
	size := consts.IntLen + codec.CummSize(txs)
	p := codec.NewWriter(size, consts.NetworkSizeLimit)
	p.PackInt(len(txs))
	for _, tx := range txs {
		if err := tx.Marshal(p); err != nil {
			return nil, err
		}
	}
	return p.Bytes(), p.Err()
}

func UnmarshalTxs(
	raw []byte,
	initialCapacity int,
	actionRegistry ActionRegistry,
	authRegistry AuthRegistry,
) (map[uint8]int, []*Transaction, error) {
	p := codec.NewReader(raw, consts.NetworkSizeLimit)
	txCount := p.UnpackInt(true)
	authCounts := map[uint8]int{}
	txs := make([]*Transaction, 0, initialCapacity) // DoS to set size to txCount
	for i := 0; i < txCount; i++ {
		tx, err := UnmarshalTx(p, actionRegistry, authRegistry)
		if err != nil {
			return nil, nil, err
		}
		txs = append(txs, tx)
		authCounts[tx.Auth.GetTypeID()]++
	}
	if !p.Empty() {
		// Ensure no leftover bytes
		return nil, nil, ErrInvalidObject
	}
	return authCounts, txs, p.Err()
}

func UnmarshalTx(
	p *codec.Packer,
	actionRegistry *codec.TypeParser[Action, *warp.Message, bool],
	authRegistry *codec.TypeParser[Auth, *warp.Message, bool],
) (*Transaction, error) {
	start := p.Offset()
	base, err := UnmarshalBase(p)
	if err != nil {
		return nil, fmt.Errorf("%w: could not unmarshal base", err)
	}
	var warpBytes []byte
	p.UnpackBytes(MaxWarpMessageSize, false, &warpBytes)
	var warpMessage *warp.Message
	var numWarpSigners int
	if len(warpBytes) > 0 {
		msg, err := warp.ParseMessage(warpBytes)
		if err != nil {
			return nil, fmt.Errorf("%w: could not unmarshal warp message", err)
		}
		if len(msg.Payload) == 0 {
			return nil, ErrEmptyWarpPayload
		}
		warpMessage = msg
		numSigners, err := msg.Signature.NumSigners()
		if err != nil {
			return nil, fmt.Errorf("%w: could not calculate number of warp signers", err)
		}
		numWarpSigners = numSigners
	}
	actionType := p.UnpackByte()
	unmarshalAction, actionWarp, ok := actionRegistry.LookupIndex(actionType)
	if !ok {
		return nil, fmt.Errorf("%w: %d is unknown action type", ErrInvalidObject, actionType)
	}
	if actionWarp && warpMessage == nil {
		return nil, fmt.Errorf("%w: action %d", ErrExpectedWarpMessage, actionType)
	}
	action, err := unmarshalAction(p, warpMessage)
	if err != nil {
		return nil, fmt.Errorf("%w: could not unmarshal action", err)
	}
	digest := p.Offset()
	authType := p.UnpackByte()
	unmarshalAuth, authWarp, ok := authRegistry.LookupIndex(authType)
	if !ok {
		return nil, fmt.Errorf("%w: %d is unknown auth type", ErrInvalidObject, authType)
	}
	if authWarp && warpMessage == nil {
		return nil, fmt.Errorf("%w: auth %d", ErrExpectedWarpMessage, authType)
	}
	auth, err := unmarshalAuth(p, warpMessage)
	if err != nil {
		return nil, fmt.Errorf("%w: could not unmarshal auth", err)
	}
	warpExpected := actionWarp || authWarp
	if !warpExpected && warpMessage != nil {
		return nil, ErrUnexpectedWarpMessage
	}

	var tx Transaction
	tx.Base = base
	tx.Action = action
	tx.WarpMessage = warpMessage
	tx.Auth = auth
	if err := p.Err(); err != nil {
		return nil, p.Err()
	}
	codecBytes := p.Bytes()
	tx.digest = codecBytes[start:digest]
	tx.bytes = codecBytes[start:p.Offset()] // ensure errors handled before grabbing memory
	tx.size = len(tx.bytes)
	tx.id = utils.ToID(tx.bytes)
	if tx.WarpMessage != nil {
		tx.numWarpSigners = numWarpSigners
		tx.warpID = tx.WarpMessage.ID()
	}
	return &tx, nil
}
