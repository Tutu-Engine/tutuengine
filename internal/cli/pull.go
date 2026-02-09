package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
)

func init() {
	rootCmd.AddCommand(pullCmd)
}

var pullCmd = &cobra.Command{
	Use:   "pull MODEL",
	Short: "Download a model from the TuTu registry",
	Long:  `Pull a model to run locally. Downloads the GGUF file from HuggingFace.
Supports resume — if a download is interrupted, run pull again to continue.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runPull,
}

func runPull(cmd *cobra.Command, args []string) error {
	modelName := args[0]

	d, err := daemon.New()
	if err != nil {
		return err
	}
	defer d.Close()

	fmt.Fprintf(os.Stderr, "pulling %s...\n", modelName)
	pb := newProgressBar()
	err = d.Models.Pull(modelName, pb.callback)
	if err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}
	fmt.Fprintln(os.Stderr)
	return nil
}
