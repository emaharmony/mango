# Matter Channel — Testing Guide

This document walks you through testing the Matter (IoT) integration step by step, from build to device control.

---

## Prerequisites

- Go 1.24+ installed
- Node.js v18+ installed (`node --version` to check)
- At least one Matter-compatible device (smart bulb, plug, lock, etc.)
- The device's QR code or manual pairing code (usually printed on the device or in its app)

---

## Step 1: Build Mango

```bash
cd ~/mango
go build -o mango ./cmd/app
```

**Expected:** Binary `mango` created with no errors.

**If it fails:** Run `go mod tidy` then rebuild.

---

## Step 2: Run Unit Tests

```bash
go test ./... -v
```

**Expected:** All packages pass. Matter package should show:
- `TestDomainFromEntity` ✅
- `TestIsWatched` ✅
- `TestControllerConfigDefaults` ✅
- `TestControllerIsRunning` ✅
- `TestFindControllerScript` ✅
- `TestMergeEntityID` ✅
- `TestDeviceCommand` ✅
- `TestBotConfigDefaults` ✅
- `TestMatterToolUnknownCommand` ✅

**If any fail:** Note the test name and error message.

---

## Step 3: Setup Matter Controller

```bash
./mango matter setup
```

**Expected:**
- ✅ Node.js found: `/path/to/node`
- ✅ Created directory: `~/.mango/matter`
- ✅ Controller script installed: `~/.mango/matter/matter-controller.mjs`
- 📦 Installing `@project-chip/matter-node.js`...
- ✅ matter-node.js installed

**If npm install fails:**
- Check Node.js version: `node --version` (needs v18+)
- Try manually: `cd ~/.mango/matter && npm install @project-chip/matter-node.js`
- Check npm registry access: `npm ping`

**If controller script not found:** The setup command generates a stub script. Check `~/.mango/matter/matter-controller.mjs` exists.

---

## Step 4: Enable Matter in Config

Edit `~/.mango/config.yaml` (or the config file you're using):

```yaml
matter:
  enabled: true
  node_path: node
  storage_dir: ~/.mango/matter
  port: 5540
```

**No entity_filters or agent_bindings needed** — defaults cover common device types.

---

## Step 5: Start Mango with Matter

```bash
./mango serve
```

**Expected log output:**
```
matter: native controller started (pid=XXXX, storage=~/.mango/matter, port=5540)
matter: native controller active — 0 device(s) across 8 domains
gateway: listening on /Users/ema/.mango/mango.sock
```

**If you see:**
- `matter: controller script not found — run 'mango matter setup' first` → Re-run Step 3
- `matter: start controller: exec: "node": executable file not found` → Install Node.js or set `node_path` in config
- `matter-controller: Error: Cannot find module '@project-chip/matter-node.js'` → npm install failed, retry manually in `~/.mango/matter`

---

## Step 6: Test Gateway API (no device yet)

In a separate terminal:

```bash
# Check matter status
curl --unix-socket ~/.mango/mango.sock http://localhost/matter
```

**Expected:**
```json
{"connected":true,"devices":{}}
```

**If you get `{"connected":false,"devices":[]}`:** The controller subprocess may have crashed. Check the Mango serve logs for `matter-controller` errors.

```bash
# Check a specific device (should 404 since none commissioned)
curl --unix-socket ~/.mango/mango.sock http://localhost/matter/light.test
```

**Expected:**
```json
{"error":"entity not found"}
```

---

## Step 7: Commission a Matter Device

Put your Matter device in pairing mode (usually by holding its button for 5-10 seconds until it blinks). Then:

```bash
# Via CLI
./mango matter commission <YOUR_QR_OR_PAIRING_CODE>
```

Or via API:
```bash
curl --unix-socket ~/.mango/mango.sock \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"code":"<YOUR_PAIRING_CODE>"}' \
  http://localhost/matter/commission
```

**Expected:**
- ✅ Device commissioned successfully!
- Matter serve logs show: `matter: Matter device <entity_id> is now on`

**If commissioning fails:**
- Ensure device is in pairing mode (blinking/pulsing)
- Ensure device is on the same network (Wi-Fi or Thread border router)
- Check the pairing code — QR codes are long strings, manual codes are short numeric
- Check logs for `matter-controller error: Commissioning failed: ...`
- The device may need to be factory-reset first if it was previously commissioned elsewhere

---

## Step 8: View Devices

```bash
# CLI list
./mango matter list
```

**Expected:**
```
DEVICE              STATE        FRIENDLY NAME
light.living_room   on           Living Room Light
```

```bash
# Device detail
./mango matter state light.living_room
```

```bash
# API
curl --unix-socket ~/.mango/mango.sock http://localhost/matter
```

---

## Step 9: Test TUI Dashboard

```bash
./mango matter dashboard
```

**Expected:**
- Full-screen TUI with orange title bar
- `● Connected` status
- Device list with entity IDs, states, friendly names
- Detail panel when you select a device
- Auto-refreshes every 5 seconds

**Controls:**
- `↑/k` `↓/j` — navigate
- `r` — force refresh
- `q` — quit

---

## Step 10: Control a Device

### Via CLI:
```bash
./mango matter list                           # see device IDs
./mango matter state light.living_room        # check current state
```

### Via agent tool (submit a task):
```bash
./mango task submit "Turn off light.living_room" --wait
```

### Via API:
```bash
# Turn off
curl --unix-socket ~/.mango/mango.sock \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"goal":"Turn off light.living_room"}' \
  http://localhost/tasks

# Check task result
curl --unix-socket ~/.mango/mango.sock http://localhost/tasks/<TASK_ID>
```

---

## Step 11: Test Device State Changes

1. Physically toggle your device (press its button)
2. Watch the Mango serve logs — should show: `matter: Matter device <id> is now <state> -> dispatching to agent "worker"`
3. Check the dashboard: `./mango matter dashboard` — state should update within 5 seconds

---

## Step 12: Test LLM Config Defaults

Verify that env var overrides work:

```bash
# Override model via env
MANGO_LLM_MODEL=qwen3:latest ./mango serve
```

**Expected:** Agents use `qwen3:latest` instead of default.

```bash
# Override provider
MANGO_LLM_PROVIDER=anthropic MANGO_LLM_MODEL=claude-sonnet-4-20250514 MANGO_LLM_BASE_URL=https://api.anthropic.com MANGO_LLM_API_KEY=your_key ./mango serve
```

**Expected:** Agents route to Anthropic (if key is valid).

---

## Troubleshooting Quick Reference

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| `matter: controller script not found` | Setup not run | `./mango matter setup` |
| `exec: "node": not found` | Node.js not installed | `brew install node` or set `node_path` |
| `Cannot find module '@project-chip/matter-node.js'` | npm install failed | `cd ~/.mango/matter && npm install @project-chip/matter-node.js` |
| Commissioning hangs | Device not in pairing mode | Hold device button 5-10s, try again |
| `connected: false` in API | Controller subprocess crashed | Check serve logs, restart Mango |
| Dashboard shows no devices | No devices commissioned | Run Step 7 |
| Device shows `unknown` state | Cluster not supported yet | Check matter-controller.mjs logs for unsupported device type |
| TUI looks broken | Terminal too small | Use full-screen terminal (80+ cols, 24+ rows) |

---

## What to Report

When testing, please note:
1. Which step succeeded/failed
2. Exact error messages from logs
3. Your Node.js version (`node --version`)
4. Whether `./mango matter setup` completed
5. The Matter device type you tested with (bulb, plug, lock, etc.)
6. Whether the JS controller script needed any modifications

This will help me fix any issues in the matter-controller.mjs script quickly! 🍊