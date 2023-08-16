package chain

import (
	"encoding/binary"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/consts"
	"github.com/ava-labs/hypersdk/window"
)

const (
	Bandwidth           Dimension = 0
	Compute                       = 1
	StorageRead                   = 2
	StorageCreate                 = 3
	StorageModification           = 4

	FeeDimensions = 5
	DimensionsLen = consts.Uint64Len * FeeDimensions

	dimensionSize = consts.Uint64Len + window.WindowSliceSize + consts.Uint64Len
)

type Dimension int
type Dimensions [FeeDimensions]uint64

type FeeManager struct {
	raw []byte
}

func NewFeeManager(raw []byte) *FeeManager {
	if len(raw) == 0 {
		raw = make([]byte, FeeDimensions*dimensionSize)
	}
	return &FeeManager{raw}
}

func (f *FeeManager) UnitPrice(d Dimension) uint64 {
	start := dimensionSize * d
	return binary.BigEndian.Uint64(f.raw[start : start+consts.Uint64Len])
}

func (f *FeeManager) Window(d Dimension) window.Window {
	start := dimensionSize*d + consts.Uint64Len
	return window.Window(f.raw[start : start+window.WindowSliceSize])
}

func (f *FeeManager) LastConsumed(d Dimension) uint64 {
	start := dimensionSize*d + consts.Uint64Len + window.WindowSliceSize
	return binary.BigEndian.Uint64(f.raw[start : start+consts.Uint64Len])
}

func (f *FeeManager) ComputeNext(lastTime int64, currTime int64, r Rules) (*FeeManager, error) {
	targetUnits := r.GetWindowTargetUnits()
	unitPriceChangeDenom := r.GetUnitPriceChangeDenominator()
	minUnitPrice := r.GetMinUnitPrice()
	since := int((currTime - lastTime) / consts.MillisecondsPerSecond)
	packer := codec.NewWriter(dimensionSize*FeeDimensions, consts.MaxInt)
	for i := Dimension(0); i < FeeDimensions; i++ {
		nextUnitPrice, nextUnitWindow, err := computeNextPriceWindow(
			f.Window(i),
			f.LastConsumed(i),
			f.UnitPrice(i),
			targetUnits[i],
			unitPriceChangeDenom[i],
			minUnitPrice[i],
			since,
		)
		if err != nil {
			return nil, err
		}
		packer.PackUint64(nextUnitPrice)
		packer.PackWindow(nextUnitWindow)
		packer.PackUint64(0) // must set usage after block is processed
	}
	return &FeeManager{raw: packer.Bytes()}, packer.Err()
}

func (f *FeeManager) SetUnitPrice(d Dimension, price uint64) {
	start := dimensionSize * d
	binary.BigEndian.PutUint64(f.raw[start:start+consts.Uint64Len], price)
}

func (f *FeeManager) SetLastConsumed(d Dimension, consumed uint64) {
	start := dimensionSize*d + consts.Uint64Len + window.WindowSliceSize
	binary.BigEndian.PutUint64(f.raw[start:start+consts.Uint64Len], consumed)
}

func (f *FeeManager) CanConsume(d Dimensions, l Dimensions) (bool, Dimension) {
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := math.Add64(f.LastConsumed(i), d[i])
		if err != nil {
			return false, i
		}
		if consumed > l[i] {
			return false, i
		}
	}
	return true, -1
}

func (f *FeeManager) Consume(d Dimensions) error {
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := math.Add64(f.LastConsumed(i), d[i])
		if err != nil {
			return err
		}
		f.SetLastConsumed(i, consumed)
	}
	return nil
}

func (f *FeeManager) Bytes() []byte {
	return f.raw
}

func (f *FeeManager) MaxFee(d Dimensions) (uint64, error) {
	fee := uint64(0)
	for i := Dimension(0); i < FeeDimensions; i++ {
		contribution, err := math.Mul64(f.UnitPrice(i), d[i])
		if err != nil {
			return 0, err
		}
		newFee, err := math.Add64(contribution, fee)
		if err != nil {
			return 0, err
		}
		fee = newFee
	}
	return fee, nil
}

func (f *FeeManager) UnitPrices() Dimensions {
	var d Dimensions
	for i := Dimension(0); i < FeeDimensions; i++ {
		d[i] = f.UnitPrice(i)
	}
	return d
}

func computeNextPriceWindow(
	previous window.Window,
	previousConsumed uint64,
	previousPrice uint64,
	target uint64, /* per window */
	changeDenom uint64,
	minPrice uint64,
	since int, /* seconds */
) (uint64, window.Window, error) {
	newRollupWindow, err := window.Roll(previous, since)
	if err != nil {
		return 0, window.Window{}, err
	}
	if since < window.WindowSize {
		// add in the units used by the parent block in the correct place
		// If the parent consumed units within the rollup window, add the consumed
		// units in.
		slot := window.WindowSize - 1 - since
		start := slot * consts.Uint64Len
		window.Update(&newRollupWindow, start, previousConsumed)
	}
	total := window.Sum(newRollupWindow)

	nextPrice := previousPrice
	if total > target {
		// If the parent block used more units than its target, the baseFee should increase.
		delta := total - target
		x := previousPrice * delta
		y := x / target
		baseDelta := y / changeDenom
		if baseDelta < 1 {
			baseDelta = 1
		}
		n, over := math.Add64(nextPrice, baseDelta)
		if over != nil {
			nextPrice = consts.MaxUint64
		} else {
			nextPrice = n
		}
	} else if total < target {
		// Otherwise if the parent block used less units than its target, the baseFee should decrease.
		delta := target - total
		x := previousPrice * delta
		y := x / target
		baseDelta := y / changeDenom
		if baseDelta < 1 {
			baseDelta = 1
		}

		// If [roll] is greater than [rollupWindow], apply the state transition to the base fee to account
		// for the interval during which no blocks were produced.
		// We use roll/rollupWindow, so that the transition is applied for every [rollupWindow] seconds
		// that has elapsed between the parent and this block.
		if since > window.WindowSize {
			// Note: roll/rollupWindow must be greater than 1 since we've checked that roll > rollupWindow
			baseDelta *= uint64(since / window.WindowSize)
		}
		n, under := math.Sub(nextPrice, baseDelta)
		if under != nil {
			nextPrice = 0
		} else {
			nextPrice = n
		}
	}
	if nextPrice < minPrice {
		nextPrice = minPrice
	}
	return nextPrice, newRollupWindow, nil
}

func Add(a, b Dimensions) (Dimensions, error) {
	d := Dimensions{}
	for i := Dimension(0); i < FeeDimensions; i++ {
		v, err := math.Add64(a[i], b[i])
		if err != nil {
			return Dimensions{}, err
		}
		d[i] = v
	}
	return d, nil
}

func MulSum(a, b Dimensions) (uint64, error) {
	val := uint64(0)
	for i := Dimension(0); i < FeeDimensions; i++ {
		v, err := math.Mul64(a[i], b[i])
		if err != nil {
			return 0, err
		}
		newVal, err := math.Add64(val, v)
		if err != nil {
			return 0, err
		}
		val = newVal
	}
	return val, nil
}

func (d Dimensions) Add(i Dimension, v uint64) error {
	newValue, err := math.Add64(d[i], v)
	if err != nil {
		return err
	}
	d[i] = newValue
	return nil
}

func (d Dimensions) CanAdd(a Dimensions, l Dimensions) bool {
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := math.Add64(d[i], a[i])
		if err != nil {
			return false
		}
		if consumed > l[i] {
			return false
		}
	}
	return true
}

func (d Dimensions) Bytes() ([]byte, error) {
	packer := codec.NewWriter(DimensionsLen, consts.MaxInt)
	for i := Dimension(0); i < FeeDimensions; i++ {
		packer.PackUint64(d[i])
	}
	return packer.Bytes(), packer.Err()
}

func UnpackDimensions(raw []byte) (Dimensions, error) {
	d := Dimensions{}
	packer := codec.NewReader(raw, DimensionsLen)
	for i := Dimension(0); i < FeeDimensions; i++ {
		d[i] = packer.UnpackUint64(false)
	}
	return d, packer.Err()
}
