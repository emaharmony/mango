package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/carlosmaranje/mango/internal/llm"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		return nil // already registered, skip
	}
	r.tools[t.Name()] = t
	return nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, name, input string) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}
	return t.Execute(ctx, input)
}

// Definitions converts all registered tools to the LLM tool-definition format
// so they can be included in a CompletionRequest.
func (r *Registry) Definitions() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		def := llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Returns:     t.Returns(),
		}
		for _, p := range t.Parameters() {
			def.Parameters = append(def.Parameters, llm.ToolParam{
				Name:        p.Name,
				Type:        p.Type,
				Description: p.Description,
				Required:    p.Required,
			})
		}
		out = append(out, def)
	}
	return out
}
