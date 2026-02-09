package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
	"github.com/tutu-network/tutu/internal/domain"
)

func init() {
	rootCmd.AddCommand(psCmd)
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List models currently loaded in memory",
	RunE:  runPs,
}

func runPs(cmd *cobra.Command, args []string) error {
	d, err := daemon.New()
	if err != nil {
		return err
	}
	defer d.Close()

	loaded := d.Pool.LoadedModels()
	if len(loaded) == 0 {
		fmt.Println("No models currently loaded.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE\tPROCESSOR\tEXPIRES")
	for _, m := range loaded {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			m.Name,
			domain.HumanSize(m.SizeBytes),
			m.Processor,
			m.ExpiresAt.Format("15:04:05"),
		)
	}
	return w.Flush()
}
