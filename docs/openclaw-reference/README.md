# OpenClaw → Mango Feature Port

This directory contains OpenClaw features ported to Mango for reference and adaptation.

## What was ported

### Config (`config/config.yaml`)
- Global LLM defaults with env var support (`MANGO_LLM_PROVIDER`, `MANGO_LLM_MODEL`, `MANGO_LLM_BASE_URL`)
- 2 agents: orchestrator, worker (inherit from LLM defaults)
- Discord config placeholder
- Native Matter controller config

### Agents (`config/agents/`)
- **ORCHESTRATOR.md** — task decomposition and delegation
- **WORKER.md** — general-purpose task worker

### Skills (`config/skills/`)
- **weather.md** — wttr.in weather lookups
- **github.md** — gh CLI operations
- **matter-iot.md** — native Matter/IoT device control

### Matter Channel (`internal/matter/`)
Native Matter controller — no Home Assistant dependency:
- **controller.go** — Manages matter.js subprocess, JSON stdin/stdout protocol
- **bot.go** — Channel gateway (device control, state monitoring, agent dispatch)
- **tool.go** — Agent tool (list, state, on, off, toggle, brightness, temp)
- **ha_client.go** — Legacy HA client (deprecated, kept for migration)
- **bot_test.go** — Unit tests

### matter.js Controller (`config/matter/matter-controller.mjs`)
Node.js subprocess that creates a Matter fabric, commissions devices, and reports state.
Users never leave Mango — setup, commissioning, and control all happen within the app.

### Gateway API
- `GET /matter` → all devices + connection status
- `GET /matter/<entity_id>` → single device state
- `POST /matter/commission` → commission a new device with QR/pairing code

### TUI Dashboard
- `mango matter dashboard` — Bubble Tea interactive dashboard (auto-refresh)
- `mango matter list` — non-interactive device list
- `mango matter state <id>` — single device detail
- `mango matter setup` — one-time dependency installation
- `mango matter commission <code>` — add new Matter device

## Key differences from OpenClaw

| Feature | OpenClaw | Mango |
|---------|----------|-------|
| Architecture | Node.js gateway + agents | Go binary + Unix socket |
| Agent system | Single persistent agent | Multi-agent with orchestrator fan-out |
| Skills | Rich tool-based skills | Markdown skill definitions appended to prompts |
| Channels | Discord, Signal, Telegram, WhatsApp, web UI | Discord + Matter (IoT) + CLI |
| Memory | Persistent memory + semantic search | SQLite key-value store |
| Scheduling | Cron jobs + heartbeats | Not yet implemented |

## Matter Setup (native, no external app)

1. `mango matter setup` — installs Node.js + matter-node.js
2. Enable in config: `matter: { enabled: true }`
3. Commission devices: `mango matter commission <QR_CODE>`
4. View: `mango matter dashboard`