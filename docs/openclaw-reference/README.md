# OpenClaw → Mango Feature Port

This directory contains OpenClaw features ported to Mango for reference and adaptation.

## What was ported

### Config (`config/config.yaml`)
- LLM provider: Ollama with `qwen3.5:cloud`
- 2 agents: orchestrator, worker
- Discord config placeholder
- Matter (IoT) channel config placeholder

### Agents (`config/agents/`)
- **ORCHESTRATOR.md** — task decomposition and delegation (Mango default format)
- **WORKER.md** — general-purpose task worker (Mango default format)

### Skills (`config/skills/`)
- **weather.md** — wttr.in weather lookups
- **github.md** — gh CLI operations
- **matter-iot.md** — Matter/IoT device control via Home Assistant

### Matter Channel (`internal/matter/`)
- **ha_client.go** — Home Assistant websocket client (auth, state, events)
- **bot.go** — Matter channel bot (device control, state monitoring, agent dispatch)
- **tool.go** — Agent tool (list, state, on, off, toggle, brightness, temp)
- **bot_test.go** — 7 passing unit tests

### Gateway API
- `GET /matter` → all devices + connection status
- `GET /matter/<entity_id>` → single device state

### TUI Dashboard
- `mango matter dashboard` — Bubble Tea interactive dashboard
- `mango matter list` — non-interactive device list
- `mango matter state <id>` — single device detail

## Key differences from OpenClaw

| Feature | OpenClaw | Mango |
|---------|----------|-------|
| Architecture | Node.js gateway + agents | Go binary + Unix socket |
| Agent system | Single persistent agent | Multi-agent with orchestrator fan-out |
| Skills | Rich tool-based skills (shell exec, cron, file ops) | Markdown skill definitions appended to prompts |
| Channels | Discord, Signal, Telegram, WhatsApp, web UI | Discord + Matter (IoT) + CLI |
| Memory | Persistent memory (MEMORY.md + semantic search) | SQLite key-value store |
| Scheduling | Cron jobs + heartbeats | Not yet implemented |
| Cost tracking | Built-in per-model cost tracking | Not available |
| Safety | Approval system for elevated commands | Not available |

## Discord Setup

Mango needs its **own** Discord bot token (separate from OpenClaw's). See [DISCORD_SETUP.md](../../DISCORD_SETUP.md) for instructions.

## Matter Setup

1. Install Home Assistant with Matter integration
2. Generate a long-lived access token in HA (Profile → Security)
3. Add to `config.yaml`:
   ```yaml
   matter:
     url: "http://homeassistant.local:8123"
     token: "YOUR_HA_LONG_LIVED_ACCESS_TOKEN"
   ```