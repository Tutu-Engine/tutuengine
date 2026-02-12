package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tutu-network/tutu/internal/daemon"
	"github.com/tutu-network/tutu/internal/infra/engine"
)

func init() {
	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run MODEL [PROMPT]",
	Short: "Run a model and start an interactive chat",
	Long:  `Run a model locally. If the model isn't downloaded yet, it will be pulled first.`,
	Args:  cobra.MinimumNArgs(1),
	RunE:  runRun,
}

func runRun(cmd *cobra.Command, args []string) error {
	modelName := args[0]

	// Optional inline prompt
	var prompt string
	if len(args) > 1 {
		prompt = args[1]
	}

	d, err := daemon.New()
	if err != nil {
		return fmt.Errorf("initialize daemon: %w", err)
	}
	defer d.Close()

	// Ensure model is available
	exists, err := d.Models.HasLocal(registry_ParseRef(modelName))
	if err != nil {
		return err
	}
	if !exists {
		fmt.Fprintf(os.Stderr, "pulling %s...\n", modelName)
		pb := newProgressBar()
		if err := d.Models.Pull(modelName, pb.callback); err != nil {
			fmt.Fprintln(os.Stderr)
			return fmt.Errorf("pull model: %w", err)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Acquire model
	handle, err := d.Pool.Acquire(modelName, engine.LoadOptions{
		NumGPULayers: -1,
		NumCtx:       4096,
	})
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	defer handle.Release()

	if prompt != "" {
		// Single-shot mode
		return generateAndPrint(cmd.Context(), handle, prompt)
	}

	// Interactive mode
	return interactiveChat(cmd.Context(), handle, modelName)
}

func generateAndPrint(ctx context.Context, handle *engine.PoolHandle, prompt string) error {
	messages := []engine.ChatMessage{
		{Role: "system", Content: "You are a helpful AI assistant."},
		{Role: "user", Content: prompt},
	}
	tokenCh, err := handle.Model().Chat(ctx, messages, engine.GenerateParams{
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   2048,
	})
	if err != nil {
		return err
	}

	for tok := range tokenCh {
		fmt.Print(tok.Text)
	}
	fmt.Println()
	return nil
}

func interactiveChat(ctx context.Context, handle *engine.PoolHandle, modelName string) error {
	fmt.Printf(">>> Chatting with %s (type /bye to exit)\n", modelName)

	// Maintain conversation history for multi-turn chat
	messages := []engine.ChatMessage{
		{Role: "system", Content: "You are a helpful AI assistant."},
	}

	scanner := newLineScanner(os.Stdin)
	for {
		fmt.Print(">>> ")
		if !scanner.Scan() {
			break
		}
		input := scanner.Text()

		if input == "/bye" || input == "/exit" || input == "/quit" {
			fmt.Println("Goodbye!")
			return nil
		}

		if input == "" {
			continue
		}

		// Add user message to history
		messages = append(messages, engine.ChatMessage{Role: "user", Content: input})

		tokenCh, err := handle.Model().Chat(ctx, messages, engine.GenerateParams{
			Temperature: 0.7,
			TopP:        0.9,
			MaxTokens:   2048,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}

		// Collect assistant response for history
		var response strings.Builder
		for tok := range tokenCh {
			fmt.Print(tok.Text)
			response.WriteString(tok.Text)
		}
		fmt.Println()
		fmt.Println()

		// Add assistant response to history
		messages = append(messages, engine.ChatMessage{Role: "assistant", Content: response.String()})
	}

	return nil
}
