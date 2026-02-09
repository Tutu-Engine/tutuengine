package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
	"github.com/tutu-network/tutu/internal/domain"
)

func init() {
	rootCmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show MODEL",
	Short: "Show detailed information about a model",
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	modelName := args[0]

	d, err := daemon.New()
	if err != nil {
		return err
	}
	defer d.Close()

	info, err := d.Models.Show(modelName)
	if err != nil {
		return err
	}

	fmt.Printf("Name:         %s\n", info.Name)
	fmt.Printf("Size:         %s\n", domain.HumanSize(info.SizeBytes))
	fmt.Printf("Format:       %s\n", info.Format)
	fmt.Printf("Family:       %s\n", info.Family)
	fmt.Printf("Parameters:   %s\n", info.Parameters)
	fmt.Printf("Quantization: %s\n", info.Quantization)
	fmt.Printf("Digest:       %s\n", info.Digest)
	fmt.Printf("Modified:     %s\n", info.PulledAt.Format("2006-01-02 15:04:05"))

	return nil
}
