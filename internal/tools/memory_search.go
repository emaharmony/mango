package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/carlosmaranje/mango/internal/memory"
)

// MemorySearchTool allows any agent to search tiered memories.
type MemorySearchTool struct {
	store memory.MemoryReader
}

func NewMemorySearchTool(store memory.MemoryReader) *MemorySearchTool {
	return &MemorySearchTool{store: store}
}

func (t *MemorySearchTool) Name() string { return "memory_search" }

func (t *MemorySearchTool) Description() string {
	return "Search tiered memories by keyword or content. Returns matching memories ordered by tier priority (persistent first) and recency."
}

func (t *MemorySearchTool) Returns() string {
	return DescribeReturnType(MemorySearchResult{})
}

func (t *MemorySearchTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "query", Type: "string", Description: "Search query - matches against content and keywords", Required: true},
		{Name: "category", Type: "string", Description: "Filter by category: project, person, decision, preference, conversation, architecture", Required: false},
		{Name: "tier", Type: "string", Description: "Filter by tier: persistent, active, cold", Required: false},
		{Name: "limit", Type: "number", Description: "Max results to return (default 10)", Required: false},
	}
}

type MemorySearchInput struct {
	Query    string `json:"query"`
	Category string `json:"category"`
	Tier     string `json:"tier"`
	Limit    int    `json:"limit"`
}

type MemorySearchResult struct {
	Total   int              `json:"total"`
	Results []MemorySummary  `json:"results"`
}

type MemorySummary struct {
	ID             string   `json:"id"`
	Content        string   `json:"content"`
	Category       string   `json:"category"`
	Tier           string   `json:"tier"`
	Keywords       []string `json:"keywords"`
	ReferenceCount int      `json:"reference_count"`
	Summary        string   `json:"summary"`
}

func (t *MemorySearchTool) Execute(ctx context.Context, input string) (string, error) {
	var req MemorySearchInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if req.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	opts := memory.SearchOpts{Category: req.Category, Tier: req.Tier, Limit: req.Limit}
	memories, err := t.store.Search(req.Query, opts)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	result := MemorySearchResult{Total: len(memories)}
	for _, m := range memories {
		result.Results = append(result.Results, MemorySummary{
			ID:             m.ID,
			Content:        m.Content,
			Category:       m.Category,
			Tier:           m.Tier,
			Keywords:       m.Keywords,
			ReferenceCount: m.ReferenceCount,
			Summary:        m.Summary,
		})
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}