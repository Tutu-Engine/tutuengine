package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
)

func init() {
	rootCmd.AddCommand(pullCmd)
}

var pullCmd = &cobra.Command{
	Use:   "pull MODEL",
	Short: "Download a model from the TuTu registry",
	Long:  `Pull a model to run locally. In Phase 0, this creates a placeholder model for testing.`,
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

	fmt.Printf("Pulling %s...\n", modelName)
	err = d.Models.Pull(modelName, func(status string, pct float64) {
		fmt.Printf("\r%-40s %3.0f%%", status, pct)
	})
	if err != nil {
		return err
	}
	fmt.Println("\nDone!")
	return nil
}
