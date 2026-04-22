# Mango Test Guide

Complete walkthrough for building, running, and testing every Mango feature on macOS.

## Prerequisites

- Go 1.22+
- Ollama running locally (`ollama serve`)
- Model pulled: `ollama pull qwen3.5:cloud` (or your preferred model)

## Build

```bash
cd ~/mango
go build -o mango ./cmd/app/
```

## 1. Gateway & Agents

```bash
# Start Mango (foreground)
./mango serve

# In another terminal:
./mango status          # Check gateway is running
./mango agent list     # List registered agents
./mango agent start orchestrator
./mango agent stop worker
```

## 2. Task Submission

```bash
./mango task submit "What is the capital of France?"
./mango task status    # Check latest task
```

## 3. Memory System

```bash
# Stats
./mango memory stats

# Store memories
./mango memory add "Project uses React + Node.js" --category project --keywords "react,nodejs" --tier persistent
./mango memory add "Ema prefers dark mode" --category preference --keywords "ui,dark-mode" --tier active

# Search
./mango memory search "React"
./mango memory search "Ema" --category preference
./mango memory search "project" --tier persistent --limit 5

# Recall (bumps reference count — use ID from search)
./mango memory recall <memory_id>
./mango memory recall <memory_id>  # again — ref count increases

# Config
./mango memory config
./mango memory config active_to_cold_days 14
./mango memory config active_to_cold_days   # verify

# Promote & decay
./mango memory promote <memory_id> persistent
./mango memory decay
./mango memory stats    # check after decay
```

## 4. Skills

Weather and GitHub are enabled on the worker by default:

```bash
./mango task submit "What's the weather in New York?"
# GitHub skill requires `gh` CLI auth
```

## 5. GoSolar Tool

```bash
./mango task submit "Calculate solar position data for NYC today at noon"
```

## 6. Matter/IoT (Optional)

Requires smart devices on the same network:

```bash
./mango matter setup   # Install Node.js matter controller
./mango matter tui     # Launch TUI dashboard
./mango matter status  # Check status without setup
```

## 7. Discord Integration (Optional)

1. Create a **separate** Discord application at [discord.com/developers/applications](https://discord.com/developers/applications)
2. Bot → copy token → enable **Message Content Intent**
3. Edit `config/config.yaml`:
   ```yaml
   discord:
     token: "YOUR_MANGO_BOT_TOKEN"
   bindings:
     - channel: "YOUR_CHANNEL_ID"
       agent: worker
   ```
4. Restart: `./mango serve`
5. Type in the bound channel — bot should respond

## 8. Config Management

```bash
./mango config          # View/edit config
./mango add             # Interactively create a new agent or skill
```

## 9. End-to-End Memory Test

Store a fact, then ask the orchestrator about it to verify auto-inject:

```bash
./mango memory add "Project Alpha uses PostgreSQL and Go" --category project --keywords "alpha,postgres,go" --tier persistent
./mango task submit "What database does Project Alpha use?"
# If auto-inject works, agent should know from injected memory context
```

## Agent Customization

Agent definitions live in `config/agents/`. Copy templates to customize:

```bash
cp config/agents/ORCHESTRATOR.template.md config/agents/ORCHESTRATOR.md
cp config/agents/WORKER.template.md config/agents/WORKER.md
# Edit the .md files with your personality, likes, dislikes, communication style
# These files are gitignored — they stay local
```

Skills live in `config/skills/`. Add `.md` files there and reference them in `config.yaml` under the agent's `skills` array.

## Smoke Test Order (Quick)

1. `./mango serve` — start gateway
2. `./mango memory stats` — verify memory system
3. `./mango memory add "hello world" --category conversation`
4. `./mango memory search "hello"`
5. `./mango task submit "What is 2+2?"` — verify agents respond
6. `./mango memory decay` — verify decay runs clean