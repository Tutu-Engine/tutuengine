package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
)

func init() {
	rootCmd.AddCommand(rmCmd)
}

var rmCmd = &cobra.Command{
	Use:   "rm MODEL",
	Short: "Remove a model from local storage",
	Args:  cobra.ExactArgs(1),
	RunE:  runRm,
}

func runRm(cmd *cobra.Command, args []string) error {
	modelName := args[0]

	d, err := daemon.New()
	if err != nil {
		return err
	}
	defer d.Close()

	if err := d.Models.Remove(modelName); err != nil {
		return err
	}

	fmt.Printf("Removed %s\n", modelName)
	return nil
}
