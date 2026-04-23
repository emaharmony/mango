package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/carlosmaranje/mango/internal/agent"
	"github.com/carlosmaranje/mango/internal/llm"
	"github.com/carlosmaranje/mango/internal/tools"
)

type Dispatcher struct {
	registry *agent.Registry
	runners  map[string]*agent.Runner
	toolReg  *tools.Registry

	mu    sync.RWMutex
	tasks map[string]*Task

	orchestrator *Orchestrator
}

func NewDispatcher(reg *agent.Registry, runners map[string]*agent.Runner, toolReg *tools.Registry, orch *Orchestrator) *Dispatcher {
	return &Dispatcher{
		registry:     reg,
		runners:      runners,
		toolReg:      toolReg,
		tasks:        make(map[string]*Task),
		orchestrator: orch,
	}
}

func newTaskID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (d *Dispatcher) Submit(ctx context.Context, goal, agentName string) (*Task, error) {
	return d.SubmitWithHistory(ctx, goal, agentName, nil)
}

func (d *Dispatcher) SubmitWithHistory(ctx context.Context, goal, agentName string, history []llm.Message) (*Task, error) {
	task := &Task{
		ID:        newTaskID(),
		Goal:      goal,
		AgentName: agentName,
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		History:   history,
	}
	d.mu.Lock()
	d.tasks[task.ID] = task
	d.mu.Unlock()

	go d.run(ctx, task)
	return task, nil
}

func (d *Dispatcher) Get(id string) (*Task, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.tasks[id]
	if !ok {
		return nil, false
	}
	taskCopy := *t
	return &taskCopy, true
}

func (d *Dispatcher) update(id string, fn func(*Task)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.tasks[id]; ok {
		fn(t)
		t.UpdatedAt = time.Now().UTC()
	}
}

func (d *Dispatcher) run(ctx context.Context, task *Task) {
	d.update(task.ID, func(t *Task) { t.Status = StatusRunning })

	if task.AgentName == "" && d.orchestrator != nil {
		result, err := d.orchestrator.Run(ctx, task.Goal, task.History, d)
		d.finalize(task.ID, result, err)
		return
	}

	result, err := d.RunOnAgentWithHistory(ctx, task.AgentName, task.Goal, task.History, false)
	d.finalize(task.ID, result, err)
}

func (d *Dispatcher) finalize(id, result string, err error) {
	d.update(id, func(t *Task) {
		if err != nil {
			t.Status = StatusFailed
			t.Error = err.Error()
			return
		}
		t.Status = StatusDone
		t.Result = result
	})
}

func (d *Dispatcher) RunOnAgent(ctx context.Context, agentName, goal string, jsonResponse bool) (string, error) {
	return d.RunOnAgentWithHistory(ctx, agentName, goal, nil, jsonResponse)
}

func (d *Dispatcher) RunOnAgentWithHistory(ctx context.Context, agentName, goal string, history []llm.Message, jsonResponse bool) (string, error) {
	// Check if agentName matches a tool — fall back to tool registry
	if d.toolReg != nil {
		if t, ok := d.toolReg.Get(agentName); ok {
			result, err := t.Execute(ctx, goal)
			if err != nil {
				return "", fmt.Errorf("tool %q execution failed: %w", agentName, err)
			}
			log.Printf("dispatcher: tool %q executed as fallback for agent %q", t.Name(), agentName)
			return result, nil
		}
	}

	runner, ok := d.runners[agentName]
	if !ok {
		return "", fmt.Errorf("no runner registered for agent %q", agentName)
	}
	if !runner.IsRunning() {
		return "", fmt.Errorf("agent %q is not running", agentName)
	}
	reply := make(chan agent.TaskResult, 1)
	runner.Submit(agent.TaskEnvelope{
		ID:      newTaskID(),
		Goal:    goal,
		Reply:   reply,
		History: history,
		JSON:    jsonResponse,
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-reply:
		return r.Result, r.Err
	}
}

func (d *Dispatcher) FanOut(ctx context.Context, steps []OrchestratedTask) []StepResult {
	var wg sync.WaitGroup
	results := make([]StepResult, len(steps))
	for i, step := range steps {
		wg.Add(1)
		go func(idx int, s OrchestratedTask) {
			defer wg.Done()
			out, err := d.RunOnAgent(ctx, s.Agent, s.Goal, s.JSON)
			results[idx] = StepResult{Agent: s.Agent, Goal: s.Goal, Result: out, Err: err}
		}(i, step)
	}
	wg.Wait()
	return results
}

func (d *Dispatcher) List() []*Task {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*Task, 0, len(d.tasks))
	for _, t := range d.tasks {
		copy := *t
		out = append(out, &copy)
	}
	return out
}
