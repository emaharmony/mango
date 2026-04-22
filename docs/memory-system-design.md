# Mango Persistent Memory — Design Document

## Overview
A tiered, human-like memory system for Mango agents. Extends the existing `internal/memory` package with structured recall, time-based decay, access-based reinforcement, auto-injection, and configurable user tracking — all with minimal invasiveness to the existing codebase.

## Design Principles
- **Least invasive**: Extend existing `memory` package, same SQLite DB, same `Store` interface pattern
- **Most reliable**: Single DB per agent, Go-level access control, no external deps
- **Manager-writes, others-read**: Only the manager agent can store/configure memories; all agents can search and recall
- **User-tracked**: Configurable list of Discord user IDs whose conversations are auto-captured for memory

## Architecture

### Database (same `memory.db`, new tables alongside existing `kv`)

```sql
-- Tiered memories
CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  content TEXT NOT NULL,
  summary TEXT,
  keywords TEXT,           -- JSON array
  category TEXT NOT NULL,  -- project, person, decision, preference, conversation, architecture
  tier TEXT NOT NULL DEFAULT 'active',  -- persistent, active, cold
  source TEXT,             -- conversation, heartbeat, manual, auto
  confidence REAL DEFAULT 1.0,
  reference_count INTEGER DEFAULT 0,
  created_at TEXT DEFAULT (datetime('now')),
  last_referenced TEXT DEFAULT (datetime('now')),
  last_tier_change TEXT DEFAULT (datetime('now')),
  expires_at TEXT
);

-- Recall tracking for reinforcement
CREATE TABLE memory_references (
  id TEXT PRIMARY KEY,
  memory_id TEXT NOT NULL,
  referenced_at TEXT DEFAULT (datetime('now')),
  context TEXT,
  FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
);

-- User-adjustable settings
CREATE TABLE decay_config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  description TEXT
);

-- Default config values
INSERT INTO decay_config VALUES
  ('active_to_cold_days', '30', 'Days before active→cold'),
  ('cold_to_prune_days', '90', 'Days before cold→prune/archive'),
  ('archive_mode', 'archive', 'prune or archive cold memories'),
  ('reference_promote_threshold', '3', 'Recalls needed for tier promotion'),
  ('tracked_users', '[]', 'Discord users whose conversations are auto-captured (empty by default)'),
  ('auto_capture', 'true', 'Automatically save important moments during conversation'),
  ('auto_inject', 'true', 'Inject relevant memories into agent context automatically'),
  ('decay_interval_hours', '168', 'Hours between decay cycles (default: weekly)');

-- Keep existing kv table untouched
```

### Access Control (Go interface segregation)

```go
// Read-only interface — all agents get this
type MemoryReader interface {
    Search(query string, opts SearchOpts) ([]Memory, error)
    Recall(id string) (Memory, error)  // increments reference_count
    Stats() (MemoryStats, error)
}

// Full interface — manager agent only
type MemoryStore interface {
    MemoryReader
    Store(content, category string, keywords []string, tier string) (string, error)
    Promote(id, tier string) error
    SetConfig(key, value string) error
    GetConfig(key string) (string, error)
    RunDecay() (DecayResult, error)
    
    // Existing kv methods (backward compatible)
    Get(key string) (string, error)
    Set(key, value string) error
    Delete(key string) error
    List(prefix string) (map[string]string, error)
    Close() error
}
```

### Auto-Inject (in `runner.go`'s `invokeLLM`)

Before sending messages to the LLM:
1. Extract keywords from the goal/task text
2. Query `memories` table for matches in `active` and `persistent` tiers
3. Prepend a context message: "Relevant memories: ..."
4. Limit injection to top-N by relevance (configurable, default 5)

This runs transparently — agents don't need to explicitly request memories.

### Auto-Capture (in `runner.go`'s task completion)

After a conversation exchange involving a tracked user:
1. Check if the exchange contains noteworthy info (decision, preference, project update, person detail)
2. If so, call `MemoryStore.Store()` with appropriate category/keywords
3. Manager agent only — triggered by the runner after task completion

### Decay Goroutine (in `memory` package)

```go
// Started when the memory store opens
go func() {
    interval := getConfigInt("decay_interval_hours", 168) * time.Hour
    ticker := time.NewTicker(interval)
    for range ticker.C {
        store.RunDecay()
    }
}()
```

Decay cycle:
1. Demote `active` → `cold` if not referenced within `active_to_cold_days`
2. Handle `cold` memories past `cold_to_prune_days`: archive to `.md` or prune based on `archive_mode`
3. Promote `cold` → `active` if `reference_count >= threshold`
4. Promote `active` → `persistent` if `reference_count >= threshold`
5. Log results to consolidation log

### Tools (registered via `tools.Registry`)

**Available to all agents (read-only):**
- `memory_search` — search memories by keyword/content
- `memory_recall` — recall a specific memory (bumps reference count)

**Manager-only:**
- `memory_store` — store a new memory
- `memory_config` — read/write decay settings

### CLI Commands

Added to `cmd/app/`:
- `mango memory stats` — tier breakdown, total counts
- `mango memory search <query>` — search memories
- `mango memory recall <id>` — recall + bump reference
- `mango memory config [key] [value]` — view/adjust settings
- `mango memory decay` — manually trigger decay cycle
- `mango memory add <content> <category> [keywords]` — manually add a memory

### User Tracking

The `tracked_users` config is a JSON array of Discord user objects. **Defaults to empty (`[]`)** — no users are tracked until explicitly configured.

Example after configuration:
```json
[
  {"id": "164169326142816256", "name": "Ema"},
  {"id": "933485619248762890", "name": "Kirby"}
]
```

When a Discord message arrives from a tracked user, the runner checks `auto_capture` setting. If enabled, after the agent responds, the runner evaluates whether the exchange contains memory-worthy content and stores it via the manager's `MemoryStore`.

User tracking can be managed via:
- `mango memory config tracked_users '[...]'`
- The `memory_config` tool (manager-only)

## Files Changed

| File | Change | Risk |
|------|--------|------|
| `internal/memory/store.go` | Add new tables, `MemoryReader`/`MemoryStore` interfaces, decay goroutine | Medium — core file, but extending not replacing |
| `internal/memory/store_test.go` | New tests for tiered operations | Low |
| `internal/agent/agent.go` | Add `MemoryReader` field for read-only access | Low |
| `internal/agent/runner.go` | Auto-inject memories into LLM context, auto-capture from tracked users | Medium — changes the prompt construction flow |
| `internal/tools/memory_search.go` | New tool file | Low — additive |
| `internal/tools/memory_recall.go` | New tool file | Low — additive |
| `internal/tools/memory_store.go` | New tool file (manager-only) | Low — additive |
| `internal/tools/memory_config.go` | New tool file (manager-only) | Low — additive |
| `cmd/app/memory.go` | New CLI commands | Low — additive |
| `cmd/app/main.go` | Register `newMemoryCmd()` | Low |

**Zero changes to:** Discord package, LLM package, skill package, matter package, existing `kv` table behavior.

## Milestones

### M1 — Core Memory System
- New tables + interfaces in `internal/memory`
- CLI commands working (`stats`, `search`, `recall`, `config`, `add`, `decay`)
- Decay goroutine running
- Backward compatible (existing tests pass)
- Seed with Ema + Kirby profiles

### M2 — Agent Integration
- `memory_search` and `memory_recall` tools registered for all agents
- `memory_store` and `memory_config` tools registered for manager only
- Read-only wrapper for non-manager agents
- Auto-inject relevant memories into LLM context

### M3 — Auto-Capture + User Tracking
- Tracked users config working
- Auto-capture after conversations with tracked users
- Integration with Discord message flow
- End-to-end: Ema says something → auto-captured → searchable → auto-injected later

### M4 — Polish + UI
- Web dashboard for memory browsing and settings (future scope)
- Performance tuning
- Rate limiting on auto-capture
- Memory deduplication