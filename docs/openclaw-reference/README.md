# OpenClaw → Mango Configuration Port

This directory contains OpenClaw workspace and skill files ported to Mango for reference and adaptation.

## What was ported

### Config (`config/config.yaml`)
- LLM provider: Ollama with `qwen3.5:cloud`
- 3 agents: orchestrator, worker, jirby
- Discord config placeholder (needs separate bot token)

### Agents (`config/agents/`)
- **ORCHESTRATOR.md** — task decomposition and delegation (Mango default format)
- **WORKER.md** — general-purpose task worker (Mango default format)
- **JIRBY.md** — Jirby persona ported from OpenClaw's SOUL.md + IDENTITY.md + USER.md

### Skills (`config/skills/`)
- **empathy.md** — ADHD-aware empathy support
- **project-management.md** — milestone-based project workflow
- **adhd-aware.md** — focus and overwhelm reduction
- **weather.md** — wttr.in weather lookups
- **github.md** — gh CLI operations

### Reference (`docs/openclaw-reference/`)
- Original SOUL.md, IDENTITY.md, USER.md, AGENTS.md, HEARTBEAT.md
- OpenClaw skill SKILL.md files for: discord, github, healthcheck, weather, taskflow, skill-creator

## Key differences from OpenClaw

| Feature | OpenClaw | Mango |
|---------|----------|-------|
| Architecture | Node.js gateway + agents | Go binary + Unix socket |
| Agent system | Single persistent agent (Jirby) | Multi-agent with orchestrator fan-out |
| Skills | Rich tool-based skills (shell exec, cron, file ops) | Markdown skill definitions appended to prompts |
| Channels | Discord, Signal, Telegram, WhatsApp, web UI | Discord + CLI only |
| Memory | Persistent memory (MEMORY.md + semantic search) | SQLite key-value store |
| Scheduling | Cron jobs + heartbeats | Not yet implemented |
| Cost tracking | Built-in per-model cost tracking | Not available |
| Safety | Approval system for elevated commands | Not available |

## Discord Setup

Mango needs its **own** Discord bot token (separate from OpenClaw's). See [DISCORD_SETUP.md](../../DISCORD_SETUP.md) for instructions.

1. Create a new Discord app at https://discord.com/developers/applications
2. Generate a bot token
3. Add to `config/config.yaml`:
   ```yaml
   discord:
     token: "YOUR_MANGO_BOT_TOKEN"
   ```