package cli

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
)

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "", "Host to listen on (overrides config)")
	serveCmd.Flags().IntVar(&servePort, "port", 0, "Port to listen on (overrides config)")
	rootCmd.AddCommand(serveCmd)
}

var (
	serveHost string
	servePort int
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the TuTu API server",
	Long:  `Start the OpenAI-compatible API server at localhost:11434.`,
	RunE:  runServe,
}

func runServe(cmd *cobra.Command, args []string) error {
	d, err := daemon.New()
	if err != nil {
		return err
	}

	// Override config from flags
	if serveHost != "" {
		d.Config.API.Host = serveHost
	}
	if servePort > 0 {
		d.Config.API.Port = servePort
	}

	return d.Serve(context.Background())
}
