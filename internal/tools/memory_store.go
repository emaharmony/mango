package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/carlosmaranje/mango/internal/memory"
)

// MemoryStoreTool allows the manager agent to store new memories.
// Non-manager agents should NOT have this tool registered.
type MemoryStoreTool struct {
	store memory.Store
}

func NewMemoryStoreTool(store memory.Store) *MemoryStoreTool {
	return &MemoryStoreTool{store: store}
}

func (t *MemoryStoreTool) Name() string { return "memory_store" }

func (t *MemoryStoreTool) Description() string {
	return "Store a new memory in the persistent tiered memory system. Manager-only tool. Categories: project, person, decision, preference, conversation, architecture. Tiers: persistent (never decays), active (decays to cold after 30 days), cold (pruned after 90 days)."
}

func (t *MemoryStoreTool) Returns() string {
	return DescribeReturnType(MemoryStoreResult{})
}

func (t *MemoryStoreTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "content", Type: "string", Description: "The memory content to store", Required: true},
		{Name: "category", Type: "string", Description: "Category: project, person, decision, preference, conversation, architecture", Required: true},
		{Name: "keywords", Type: "string", Description: "Comma-separated keywords for search", Required: false},
		{Name: "tier", Type: "string", Description: "Tier: persistent, active, cold (default: active)", Required: false},
	}
}

type MemoryStoreInput struct {
	Content  string `json:"content"`
	Category string `json:"category"`
	Keywords string `json:"keywords"`
	Tier     string `json:"tier"`
}

type MemoryStoreResult struct {
	ID       string   `json:"id"`
	Content  string   `json:"content"`
	Category string   `json:"category"`
	Tier     string   `json:"tier"`
	Keywords []string `json:"keywords"`
}

func (t *MemoryStoreTool) Execute(ctx context.Context, input string) (string, error) {
	var req MemoryStoreInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}
	if req.Content == "" {
		return "", fmt.Errorf("content is required")
	}
	if req.Category == "" {
		req.Category = "conversation"
	}
	if req.Tier == "" {
		req.Tier = "active"
	}

	var keywords []string
	if req.Keywords != "" {
		for _, kw := range strings.Split(req.Keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				keywords = append(keywords, kw)
			}
		}
	}

	id, err := t.store.StoreMemory(req.Content, req.Category, keywords, req.Tier)
	if err != nil {
		return "", fmt.Errorf("store failed: %w", err)
	}

	result := MemoryStoreResult{
		ID:       id,
		Content:  req.Content,
		Category: req.Category,
		Tier:     req.Tier,
		Keywords: keywords,
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}