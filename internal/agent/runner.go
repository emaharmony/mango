package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/carlosmaranje/mango/internal/llm"
	"github.com/carlosmaranje/mango/internal/memory"
	"github.com/carlosmaranje/mango/internal/tools"
)

type TaskEnvelope struct {
	ID       string
	Goal     string
	Reply    chan<- TaskResult
	Metadata map[string]string
	History  []llm.Message
	JSON     bool
}

type TaskResult struct {
	ID     string
	Result string
	Err    error
}

type Runner struct {
	Agent    *Agent
	Interval time.Duration
	toolReg  *tools.Registry

	taskCh chan TaskEnvelope

	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	stopDone chan struct{}
}

func NewRunner(a *Agent, toolReg *tools.Registry, interval time.Duration) *Runner {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Runner{
		Agent:    a,
		Interval: interval,
		toolReg:  toolReg,
		taskCh:   make(chan TaskEnvelope, 64),
	}
}

func (r *Runner) Submit(env TaskEnvelope) {
	r.taskCh <- env
}

func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

func (r *Runner) Start(parent context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return fmt.Errorf("runner for %q already running", r.Agent.Name)
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.running = true
	r.stopDone = make(chan struct{})
	go r.loop(ctx)
	return nil
}

func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	cancel := r.cancel
	done := r.stopDone
	r.mu.Unlock()

	cancel()
	<-done
}

func (r *Runner) loop(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		r.running = false
		close(r.stopDone)
		r.mu.Unlock()
	}()

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case env := <-r.taskCh:
			go r.executeTask(ctx, env)
		case <-ticker.C:
			r.heartbeat(ctx)
		}
	}
}

func (r *Runner) heartbeat(_ context.Context) {
	if r.Agent.Memory == nil {
		return
	}
	_ = r.Agent.Memory.Set("heartbeat/last", time.Now().UTC().Format(time.RFC3339))
}

func (r *Runner) executeTask(ctx context.Context, env TaskEnvelope) {
	result, err := r.invokeLLM(ctx, env.Goal, env.History, env.JSON)
	if env.Reply != nil {
		select {
		case env.Reply <- TaskResult{ID: env.ID, Result: result, Err: err}:
		case <-ctx.Done():
		}
	}
}

func (r *Runner) invokeLLM(ctx context.Context, goal string, history []llm.Message, jsonResponse bool) (string, error) {
	if r.Agent.LLM == nil {
		return "", fmt.Errorf("agent %q has no LLM client", r.Agent.Name)
	}
	if r.Agent.SystemPrompt == "" {
		return "", fmt.Errorf("agent %q has no system prompt", r.Agent.Name)
	}

	messages := []llm.Message{{Role: "system", Content: r.Agent.SystemPrompt}}

	// Auto-inject relevant memories if enabled
	if r.Agent.Memory != nil {
		if mem, ok := r.Agent.Memory.(memory.Store); ok {
			if autoInject, _ := mem.GetConfig("auto_inject"); autoInject != "false" {
				memories := r.injectRelevantMemories(goal, mem)
				if len(memories) > 0 {
					var parts []string
					for _, m := range memories {
						parts = append(parts, fmt.Sprintf("- [%s/%s] %s", m.Tier, m.Category, m.Content))
					}
					injectMsg := fmt.Sprintf("Relevant memories:\n%s", strings.Join(parts, "\n"))
					messages = append(messages, llm.Message{Role: "system", Content: injectMsg})
				}
			}
		}
	}

	if len(history) > 0 {
		messages = append(messages, history...)
	} else if r.Agent.Session != nil {
		messages = append(messages, r.Agent.Session.Snapshot()...)
	}
	messages = append(messages, llm.Message{Role: "user", Content: goal})

	var toolDefs []llm.ToolDef
	if r.toolReg != nil {
		toolDefs = r.toolReg.Definitions()
	}

	useSession := len(history) == 0 && r.Agent.Session != nil

	for step := 1; ; step++ {
		log.Printf("agent %q: step %d — sending %d messages to LLM (tools: %d)", r.Agent.Name, step, len(messages), len(toolDefs))
		resp, err := r.Agent.LLM.Complete(ctx, llm.CompletionRequest{
			Messages:  messages,
			MaxTokens: 1024,
			JSON:      jsonResponse,
			Tools:     toolDefs,
		})
		if err != nil {
			return "", err
		}

		log.Printf("agent %q: step %d — content=%q toolCalls=%d", r.Agent.Name, step, resp.Content, len(resp.ToolCalls))

		if len(resp.ToolCalls) == 0 {
			if useSession {
				r.Agent.Session.Append(llm.Message{Role: "user", Content: goal})
				r.Agent.Session.Append(llm.Message{Role: "assistant", Content: resp.Content})
			}

			// Auto-capture: if enabled and this is the manager agent, evaluate storing
			r.autoCapture(goal, resp.Content)

			return resp.Content, nil
		}

		// Append the assistant turn (with tool calls) then execute each tool.
		messages = append(messages, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})
		for _, tc := range resp.ToolCalls {
			log.Printf("agent %q: step %d — tool call %q input=%s", r.Agent.Name, step, tc.Name, tc.Input)
			result, execErr := r.toolReg.Execute(ctx, tc.Name, tc.Input)
			msg := llm.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Name,
			}
			if execErr != nil {
				log.Printf("agent %q: step %d — tool %q error: %v", r.Agent.Name, step, tc.Name, execErr)
				msg.Content = "error: " + execErr.Error()
			} else {
				log.Printf("agent %q: step %d — tool %q result=%s", r.Agent.Name, step, tc.Name, result)
				msg.Content = result
			}
			messages = append(messages, msg)
		}
	}
}

// injectRelevantMemories searches for memories relevant to the given goal text.
func (r *Runner) injectRelevantMemories(goal string, mem memory.Store) []memory.Memory {
	keywords := extractKeywords(goal)
	if len(keywords) == 0 {
		return nil
	}

	var allResults []memory.Memory
	seen := map[string]bool{}

	for _, kw := range keywords {
		results, err := mem.Search(kw, memory.SearchOpts{Limit: 3})
		if err != nil {
			continue
		}
		for _, m := range results {
			if !seen[m.ID] {
				seen[m.ID] = true
				allResults = append(allResults, m)
			}
		}
	}

	if len(allResults) > 5 {
		allResults = allResults[:5]
	}
	return allResults
}

// autoCapture evaluates whether a conversation exchange should be stored as a memory.
func (r *Runner) autoCapture(userMsg, assistantMsg string) {
	if r.Agent.Memory == nil {
		return
	}
	mem, ok := r.Agent.Memory.(memory.Store)
	if !ok {
		return
	}

	autoCaptureVal, _ := mem.GetConfig("auto_capture")
	if autoCaptureVal == "false" {
		return
	}

	combined := userMsg + " " + assistantMsg
	if len(combined) < 50 {
		return
	}

	category := detectCategory(combined)
	if category == "" {
		return
	}

	summary := combined
	if len(summary) > 200 {
		summary = summary[:200]
	}

	keywords := extractKeywords(userMsg)
	if len(keywords) == 0 {
		keywords = []string{"auto-captured"}
	}

	id, err := mem.StoreMemory(summary, category, keywords, "active")
	if err != nil {
		log.Printf("auto-capture: failed to store memory: %v", err)
	} else {
		log.Printf("auto-capture: stored memory %s [%s/%s]", id, "active", category)
	}
}

// extractKeywords pulls meaningful words from text for memory search.
func extractKeywords(text string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "being": true, "have": true,
		"has": true, "had": true, "do": true, "does": true, "did": true,
		"will": true, "would": true, "could": true, "should": true, "may": true,
		"might": true, "shall": true, "can": true, "need": true, "dare": true,
		"ought": true, "used": true, "to": true, "of": true, "in": true,
		"for": true, "on": true, "with": true, "at": true, "by": true,
		"from": true, "as": true, "into": true, "through": true, "during": true,
		"before": true, "after": true, "above": true, "below": true,
		"between": true, "out": true, "off": true, "over": true, "under": true,
		"again": true, "further": true, "then": true, "once": true, "here": true,
		"there": true, "when": true, "where": true, "why": true, "how": true,
		"all": true, "both": true, "each": true, "few": true, "more": true,
		"most": true, "other": true, "some": true, "such": true, "no": true,
		"nor": true, "not": true, "only": true, "own": true, "same": true,
		"so": true, "than": true, "too": true, "very": true, "just": true,
		"because": true, "but": true, "and": true, "or": true, "if": true,
		"that": true, "this": true, "it": true, "its": true, "i": true,
		"me": true, "my": true, "we": true, "you": true, "your": true,
		"he": true, "she": true, "they": true, "them": true, "what": true,
	}

	words := strings.Fields(strings.ToLower(text))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ",.!?;:\"'()[]{}")
		if len(w) > 2 && !stopWords[w] {
			keywords = append(keywords, w)
		}
	}
	if len(keywords) > 5 {
		keywords = keywords[:5]
	}
	return keywords
}

// detectCategory infers a memory category from content.
func detectCategory(text string) string {
	lower := strings.ToLower(text)

	decisionWords := []string{"decided", "decision", "let's go with", "we'll use", "chosen", "going with"}
	for _, w := range decisionWords {
		if strings.Contains(lower, w) {
			return "decision"
		}
	}

	prefWords := []string{"prefer", "i want", "i like", "i don't like", "always ask", "never do", "must have"}
	for _, w := range prefWords {
		if strings.Contains(lower, w) {
			return "preference"
		}
	}

	archWords := []string{"architecture", "refactor", "migration", "database", "schema", "endpoint", "api", "system design"}
	for _, w := range archWords {
		if strings.Contains(lower, w) {
			return "architecture"
		}
	}

	projWords := []string{"project", "milestone", "sprint", "release", "deploy", "feature", "bug", "fix"}
	for _, w := range projWords {
		if strings.Contains(lower, w) {
			return "project"
		}
	}

	return ""
}
