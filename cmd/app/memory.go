package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/carlosmaranje/mango/internal/memory"
	"github.com/spf13/cobra"
)

func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage Mango's persistent memory system",
	}

	cmd.AddCommand(
		newMemoryStatsCmd(),
		newMemorySearchCmd(),
		newMemoryRecallCmd(),
		newMemoryAddCmd(),
		newMemoryConfigCmd(),
		newMemoryDecayCmd(),
		newMemoryPromoteCmd(),
	)

	return cmd
}

func memoryDirFromConfig() string {
	home, _ := os.UserHomeDir()
	return home + "/.mango/memory"
}

func openMemoryStore() (memory.Store, error) {
	return memory.Open(memoryDirFromConfig())
}

func newMemoryStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show memory tier and category breakdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openMemoryStore()
			if err != nil {
				return err
			}
			defer store.Close()

			stats, err := store.Stats()
			if err != nil {
				return err
			}

			fmt.Printf("Total: %d memories\n\n", stats.Total)
			fmt.Println("By Tier:")
			for _, tier := range []string{"persistent", "active", "cold"} {
				fmt.Printf("  %s: %d\n", tier, stats.TierCounts[tier])
			}
			fmt.Println("\nBy Category:")
			for cat, count := range stats.CategoryCount {
				fmt.Printf("  %s: %d\n", cat, count)
			}
			return nil
		},
	}
}

func newMemorySearchCmd() *cobra.Command {
	var category, tier string
	var limit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search memories by keyword or content",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openMemoryStore()
			if err != nil {
				return err
			}
			defer store.Close()

			opts := memory.SearchOpts{Category: category, Tier: tier, Limit: limit}
			results, err := store.Search(args[0], opts)
			if err != nil {
				return err
			}

			if len(results) == 0 {
				fmt.Println("No memories found.")
				return nil
			}

			for _, m := range results {
				preview := m.Content
				if len(preview) > 80 {
					preview = preview[:80] + "..."
				}
				fmt.Printf("%s [%s/%s] (refs:%d) %s\n", m.ID[:20], m.Tier, m.Category, m.ReferenceCount, preview)
			}
			fmt.Printf("\n%d memories found\n", len(results))
			return nil
		},
	}

	cmd.Flags().StringVar(&category, "category", "", "Filter by category")
	cmd.Flags().StringVar(&tier, "tier", "", "Filter by tier (persistent/active/cold)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Max results")

	return cmd
}

func newMemoryRecallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recall <id>",
		Short: "Recall a memory (bumps reference count)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openMemoryStore()
			if err != nil {
				return err
			}
			defer store.Close()

			m, err := store.Recall(args[0])
			if err != nil {
				return err
			}

			kw, _ := json.Marshal(m.Keywords)
			fmt.Printf("ID:              %s\n", m.ID)
			fmt.Printf("Content:         %s\n", m.Content)
			fmt.Printf("Summary:         %s\n", m.Summary)
			fmt.Printf("Keywords:        %s\n", string(kw))
			fmt.Printf("Category:        %s\n", m.Category)
			fmt.Printf("Tier:            %s\n", m.Tier)
			fmt.Printf("References:      %d\n", m.ReferenceCount)
			fmt.Printf("Created:         %s\n", m.CreatedAt)
			fmt.Printf("Last Referenced: %s\n", m.LastReferenced)
			return nil
		},
	}
}

func newMemoryAddCmd() *cobra.Command {
	var category, tier, keywords string

	cmd := &cobra.Command{
		Use:   "add <content>",
		Short: "Store a new memory",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openMemoryStore()
			if err != nil {
				return err
			}
			defer store.Close()

			kw := []string{}
			if keywords != "" {
				kw = strings.Split(keywords, ",")
			}
			if tier == "" {
				tier = "active"
			}
			if category == "" {
				category = "conversation"
			}

			id, err := store.StoreMemory(strings.Join(args, " "), category, kw, tier)
			if err != nil {
				return err
			}

			fmt.Printf("Memory stored: %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringVar(&category, "category", "conversation", "Memory category")
	cmd.Flags().StringVar(&tier, "tier", "active", "Memory tier (persistent/active/cold)")
	cmd.Flags().StringVar(&keywords, "keywords", "", "Comma-separated keywords")

	return cmd
}

func newMemoryConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config [key] [value]",
		Short: "View or adjust memory settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openMemoryStore()
			if err != nil {
				return err
			}
			defer store.Close()

			if len(args) == 0 {
				for _, key := range []string{
					"active_to_cold_days", "cold_to_prune_days", "archive_mode",
					"reference_promote_threshold", "tracked_users",
					"auto_capture", "auto_inject", "decay_interval_hours",
				} {
					val, _ := store.GetConfig(key)
					fmt.Printf("%s = %s\n", key, val)
				}
				return nil
			}

			if len(args) == 1 {
				val, _ := store.GetConfig(args[0])
				fmt.Printf("%s = %s\n", args[0], val)
				return nil
			}

			if err := store.SetConfig(args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Updated %s = %s\n", args[0], args[1])
			return nil
		},
	}
}

func newMemoryDecayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "decay",
		Short: "Manually trigger a decay cycle",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openMemoryStore()
			if err != nil {
				return err
			}
			defer store.Close()

			result, err := store.RunDecay()
			if err != nil {
				return err
			}

			fmt.Printf("Decay complete:\n")
			fmt.Printf("  Demoted to cold:        %d\n", result.DemotedToCold)
			fmt.Printf("  Promoted to active:     %d\n", result.PromotedToActive)
			fmt.Printf("  Promoted to persistent: %d\n", result.PromotedToPersistent)
			fmt.Printf("  Archived/Pruned:        %d\n", result.ArchivedOrPruned)
			return nil
		},
	}
}

func newMemoryPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <id> <tier>",
		Short: "Manually promote a memory to a specific tier",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openMemoryStore()
			if err != nil {
				return err
			}
			defer store.Close()

			if err := store.Promote(args[0], args[1]); err != nil {
				return err
			}

			fmt.Printf("Memory %s promoted to %s\n", args[0], args[1])
			return nil
		},
	}
}