// Package cli implements the TuTu command-line interface using Cobra.
// Each subcommand maps to a Phase 0 capability (run, pull, list, etc.).
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tutu",
	Short: "TuTu — Run AI models locally",
	Long: `TuTu is the local-first AI runtime.
Run large language models on your machine with zero network, zero accounts.

Phase 0 (Spark): Single node, full inference, OpenAI-compatible API.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command. Called from main.go.
func Execute(version string) {
	rootCmd.Version = version

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
