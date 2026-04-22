package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/carlosmaranje/mango/internal/memory"
)

// MemoryConfigTool allows the manager agent to view and adjust memory system settings.
// Non-manager agents should NOT have this tool registered.
type MemoryConfigTool struct {
	store memory.Store
}

func NewMemoryConfigTool(store memory.Store) *MemoryConfigTool {
	return &MemoryConfigTool{store: store}
}

func (t *MemoryConfigTool) Name() string { return "memory_config" }

func (t *MemoryConfigTool) Description() string {
	return "View or adjust persistent memory system settings. Manager-only tool. Call with no args to see all settings, with one arg to see a specific setting, or with key+value to update."
}

func (t *MemoryConfigTool) Returns() string {
	return DescribeReturnType(MemoryConfigResult{})
}

func (t *MemoryConfigTool) Parameters() []Parameter {
	return []Parameter{
		{Name: "key", Type: "string", Description: "Config key to view or set", Required: false},
		{Name: "value", Type: "string", Description: "New value to set (omit to just view)", Required: false},
	}
}

type MemoryConfigInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type MemoryConfigResult struct {
	Action string            `json:"action"` // "list", "get", "set"
	Key    string            `json:"key,omitempty"`
	Value  string            `json:"value,omitempty"`
	All    map[string]string `json:"all,omitempty"`
}

func (t *MemoryConfigTool) Execute(ctx context.Context, input string) (string, error) {
	var req MemoryConfigInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// List all config
	if req.Key == "" {
		allSettings := map[string]string{}
		for _, key := range []string{
			"active_to_cold_days", "cold_to_prune_days", "archive_mode",
			"reference_promote_threshold", "tracked_users",
			"auto_capture", "auto_inject", "decay_interval_hours",
		} {
			val, _ := t.store.GetConfig(key)
			allSettings[key] = val
		}
		result := MemoryConfigResult{Action: "list", All: allSettings}
		out, _ := json.MarshalIndent(result, "", "  ")
		return string(out), nil
	}

	// Set config
	if req.Value != "" {
		if err := t.store.SetConfig(req.Key, req.Value); err != nil {
			return "", fmt.Errorf("set config failed: %w", err)
		}
		result := MemoryConfigResult{Action: "set", Key: req.Key, Value: req.Value}
		out, _ := json.MarshalIndent(result, "", "  ")
		return string(out), nil
	}

	// Get single config
	val, err := t.store.GetConfig(req.Key)
	if err != nil {
		return "", fmt.Errorf("get config failed: %w", err)
	}
	result := MemoryConfigResult{Action: "get", Key: req.Key, Value: val}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}