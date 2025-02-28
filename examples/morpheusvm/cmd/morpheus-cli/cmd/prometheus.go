// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//nolint:lll
package cmd

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/hypersdk/cli"
	"github.com/ava-labs/hypersdk/utils"
	"github.com/spf13/cobra"
)

var prometheusCmd = &cobra.Command{
	Use: "prometheus",
	RunE: func(*cobra.Command, []string) error {
		return ErrMissingSubcommand
	},
}

var generatePrometheusCmd = &cobra.Command{
	Use: "generate",
	RunE: func(_ *cobra.Command, args []string) error {
		return handler.Root().GeneratePrometheus(runPrometheus, prometheusFile, prometheusData, func(chainID ids.ID) []string {
			panels := []string{}

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_empty_block_built[5s])", chainID))
			utils.Outf("{{yellow}}empty blocks built (5s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_build_capped[5s])", chainID))
			utils.Outf("{{yellow}}build time capped (5s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("avalanche_%s_blks_processing", chainID))
			utils.Outf("{{yellow}}blocks processing:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_blks_accepted_count[5s])/5", chainID))
			utils.Outf("{{yellow}}blocks accepted per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_blks_rejected_count[5s])/5", chainID))
			utils.Outf("{{yellow}}blocks rejected per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_vm_txs_submitted[5s])/5", chainID))
			utils.Outf("{{yellow}}transactions submitted per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_vm_txs_gossiped[5s])/5", chainID))
			utils.Outf("{{yellow}}transactions gossiped per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_vm_txs_received[5s])/5", chainID))
			utils.Outf("{{yellow}}transactions received per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_vm_txs_verified[5s])/5", chainID))
			utils.Outf("{{yellow}}transactions verified per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_vm_txs_accepted[5s])/5", chainID))
			utils.Outf("{{yellow}}transactions accepted per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_state_operations[5s])/5", chainID))
			utils.Outf("{{yellow}}state operations per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_state_changes[5s])/5", chainID))
			utils.Outf("{{yellow}}state changes per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_root_calculated_sum[5s])/1000000/5", chainID))
			utils.Outf("{{yellow}}root calcuation wait (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_wait_signatures_sum[5s])/1000000/5", chainID))
			utils.Outf("{{yellow}}signature verification wait (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_cleared_mempool[5s])/5", chainID))
			utils.Outf("{{yellow}}cleared mempool per second:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("avalanche_%s_vm_hypersdk_chain_mempool_size", chainID))
			utils.Outf("{{yellow}}mempool size:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, "avalanche_resource_tracker_cpu_usage")
			utils.Outf("{{yellow}}CPU usage:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, "avalanche_go_memstats_alloc_bytes")
			utils.Outf("{{yellow}}memory (avalanchego) usage:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("avalanche_%s_vm_go_memstats_alloc_bytes", chainID))
			utils.Outf("{{yellow}}memory (morpheusvm) usage:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_handler_chits_sum[5s])/1000000/5 + increase(avalanche_%s_handler_notify_sum[5s])/1000000/5 + increase(avalanche_%s_handler_get_sum[5s])/1000000/5 + increase(avalanche_%s_handler_push_query_sum[5s])/1000000/5 + increase(avalanche_%s_handler_put_sum[5s])/1000000/5 + increase(avalanche_%s_handler_pull_query_sum[5s])/1000000/5 + increase(avalanche_%s_handler_query_failed_sum[5s])/1000000/5", chainID, chainID, chainID, chainID, chainID, chainID, chainID))
			utils.Outf("{{yellow}}consensus engine processing (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, cli.InboundBandwidth)
			utils.Outf("{{yellow}}inbound bandwidth (B):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, cli.InboundCompressionSavingsPercent)
			utils.Outf("{{yellow}}inbound compression savings (%):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, cli.DecompressionTime)
			utils.Outf("{{yellow}}inbound decompression time (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, cli.OutboundBandwidth)
			utils.Outf("{{yellow}}outbound bandwidth (B):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, cli.OutboundCompressionSavingsPercent)
			utils.Outf("{{yellow}}outbound compression savings (%):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, cli.CompressionTime)
			utils.Outf("{{yellow}}outbound compression time (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, cli.OutboundFailed)
			utils.Outf("{{yellow}}outbound failed:{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_block_build_sum[5s])/1000000/5", chainID))
			utils.Outf("{{yellow}}block build (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_block_parse_sum[5s])/1000000/5", chainID))
			utils.Outf("{{yellow}}block parse (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_block_verify_sum[5s])/1000000/5", chainID))
			utils.Outf("{{yellow}}block verify (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_block_accept_sum[5s])/1000000/5", chainID))
			utils.Outf("{{yellow}}block accept (ms/s):{{/}} %s\n", panels[len(panels)-1])

			panels = append(panels, fmt.Sprintf("increase(avalanche_%s_vm_hypersdk_chain_block_process_sum[5s])/1000000/5", chainID))
			utils.Outf("{{yellow}}block process [async] (ms/s):{{/}} %s\n", panels[len(panels)-1])

			return panels
		})
	},
}
