package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
)

func init() {
	rootCmd.AddCommand(stopCmd)
}

var stopCmd = &cobra.Command{
	Use:   "stop MODEL",
	Short: "Unload a model from memory",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	// In Phase 0, "stop" simply unloads all models.
	// A more granular approach (by name) will come in Phase 1.
	d, err := daemon.New()
	if err != nil {
		return err
	}
	defer d.Close()

	if err := d.Pool.UnloadAll(); err != nil {
		return err
	}

	fmt.Printf("Stopped model %s\n", args[0])
	return nil
}
