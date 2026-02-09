package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/app"
	"github.com/tutu-network/tutu/internal/daemon"
)

func init() {
	createCmd.Flags().StringVarP(&createFile, "file", "f", "TuTufile", "Path to TuTufile")
	rootCmd.AddCommand(createCmd)
}

var createFile string

var createCmd = &cobra.Command{
	Use:   "create MODEL",
	Short: "Create a model from a TuTufile",
	Long: `Create a custom model from a TuTufile.

Example TuTufile:
  FROM llama3.2
  PARAMETER temperature 0.8
  SYSTEM "You are a helpful assistant."`,
	Args: cobra.ExactArgs(1),
	RunE: runCreate,
}

func runCreate(cmd *cobra.Command, args []string) error {
	modelName := args[0]

	data, err := os.ReadFile(createFile)
	if err != nil {
		return fmt.Errorf("read TuTufile: %w", err)
	}

	tf, err := app.ParseTuTufile(strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("parse TuTufile: %w", err)
	}

	d, err := daemon.New()
	if err != nil {
		return err
	}
	defer d.Close()

	if err := d.Models.CreateFromTuTufile(modelName, *tf); err != nil {
		return err
	}

	fmt.Printf("Created model %s from %s\n", modelName, tf.From)
	return nil
}
