# Matter (IoT) Skill

Control smart home devices natively through Mango's built-in Matter controller.
No external apps needed — devices are commissioned and managed entirely within Mango.

## Commands
- `list` — Show all Matter devices and their current states
- `state <entity_id>` — Get detailed state of a specific device
- `on <entity_id>` — Turn on a device
- `off <entity_id>` — Turn off a device
- `toggle <entity_id>` — Toggle a device on/off
- `brightness <entity_id> <0-255>` — Set a light's brightness
- `temp <entity_id> <value>` — Set a thermostat's temperature

## Entity ID Format
Devices use `domain.name` format:
- `light.living_room` — a light
- `switch.kitchen_outlet` — a smart plug/switch
- `sensor.temperature_living` — a temperature sensor
- `lock.front_door` — a smart lock
- `climate.thermostat` — a thermostat

## Setup
1. Run `mango matter setup` — installs Node.js dependencies
2. Enable in config.yaml: `matter: { enabled: true }`
3. Commission devices: `mango matter commission <QR_CODE>`
4. View dashboard: `mango matter dashboard`

## Requirements
- Node.js (v18+)
- `mango matter setup` must be run once before starting