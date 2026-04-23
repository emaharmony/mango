package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/carlosmaranje/mango/internal/memory"
)

// MemoryRecallTool allows any agent to recall a specific memory and bump its reference count.
type MemoryRecallTool struct {
	store memory.MemoryReader
}

func NewMemoryRecallTool(store memory.MemoryReader) *MemoryRecallTool {
	return &MemoryRecallTool{store: store}
}

func (t *MemoryRecallTool) Name() string { return "memory_recall" }

func (t *MemoryRecallTool) Description() string {
	return "Recall a specific memory by ID. Increments the reference count, which can promote the memory to a higher tier. Use this when you want full details about a memory found via search."
}

func (t *MemoryRecallTool) Returns() string {
	return DescribeReturnType(MemoryRecallResult{})
}

func (t *MemoryRecallTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "id", Type: "string", Description: "The memory ID to recall", Required: true},
	}
}

type MemoryRecallInput struct {
	ID string `json:"id"`
}

type MemoryRecallResult struct {
	ID             string   `json:"id"`
	Content        string   `json:"content"`
	Summary        string   `json:"summary"`
	Category       string   `json:"category"`
	Tier           string   `json:"tier"`
	Keywords       []string `json:"keywords"`
	Source         string   `json:"source"`
	ReferenceCount int      `json:"reference_count"`
	CreatedAt      string   `json:"created_at"`
	LastReferenced string   `json:"last_referenced"`
	Promoted       string   `json:"promoted,omitempty"` // set if tier changed during recall
}

func (t *MemoryRecallTool) Execute(ctx context.Context, input string) (string, error) {
	var req MemoryRecallInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if req.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	m, err := t.store.Recall(req.ID)
	if err != nil {
		return "", fmt.Errorf("recall failed: %w", err)
	}

	result := MemoryRecallResult{
		ID:             m.ID,
		Content:        m.Content,
		Summary:        m.Summary,
		Category:       m.Category,
		Tier:           m.Tier,
		Keywords:       m.Keywords,
		Source:         m.Source,
		ReferenceCount: m.ReferenceCount,
		CreatedAt:      m.CreatedAt,
		LastReferenced: m.LastReferenced,
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}