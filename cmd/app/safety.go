package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/carlosmaranje/mango/internal/agent"
)

func newSafetyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "safety",
		Short: "Manage safety rules for agent actions",
	}
	cmd.AddCommand(newSafetyRulesCmd())
	return cmd
}

func newSafetyRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rules",
		Short: "List active safety rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			checker := agent.NewSafetyChecker()
			rules := checker.Rules()

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tDESCRIPTION")
			for _, r := range rules {
				fmt.Fprintf(tw, "%s\t%s\n", r.Name, r.Description)
			}
			return tw.Flush()
		},
	}
}