package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/carlosmaranje/mango/internal/constants"
)

var configPath string

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	root := &cobra.Command{
		Use:           constants.AppName,
		Short:         "Agent Gateway — multi-agent orchestration with Discord and a CLI control plane",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", fmt.Sprintf("path to config.yaml (default: /etc/%s/config.yaml or ./config.yaml, can be overridden by MANGO_CONFIG)", constants.AppName))

	root.AddCommand(
		newServeCmd(),
		newStatusCmd(),
		newAddCmd(),
		newAgentCmd(),
		newTaskCmd(),
		newConfigCmd(),
		newMatterCmd(),
		newMemoryCmd(),
		newSafetyCmd(),
	)

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
