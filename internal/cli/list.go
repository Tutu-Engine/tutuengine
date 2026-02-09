package cli

import (
	"fmt"
	"text/tabwriter"
	"os"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
	"github.com/tutu-network/tutu/internal/domain"
)

func init() {
	rootCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List locally available models",
	RunE:    runList,
}

func runList(cmd *cobra.Command, args []string) error {
	d, err := daemon.New()
	if err != nil {
		return err
	}
	defer d.Close()

	models, err := d.Models.List()
	if err != nil {
		return err
	}

	if len(models) == 0 {
		fmt.Println("No models installed. Run 'tutu pull <model>' to get started.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE\tQUANTIZATION\tMODIFIED")
	for _, m := range models {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			m.Name,
			domain.HumanSize(m.SizeBytes),
			m.Quantization,
			m.PulledAt.Format("2006-01-02 15:04"),
		)
	}
	return w.Flush()
}
