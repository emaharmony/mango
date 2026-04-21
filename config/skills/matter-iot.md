# Matter (IoT) Skill

Control smart home devices via the Matter protocol through Home Assistant.

## Commands
- `list` — Show all Matter devices and their current states
- `state <entity_id>` — Get detailed state of a specific device (e.g., `state light.living_room`)
- `on <entity_id>` — Turn on a device
- `off <entity_id>` — Turn off a device
- `toggle <entity_id>` — Toggle a device on/off
- `brightness <entity_id> <0-255>` — Set a light's brightness
- `temp <entity_id> <value>` — Set a climate device's temperature

## Entity ID Format
Home Assistant uses `domain.name` format:
- `light.living_room` — a light
- `switch.kitchen_outlet` — a smart plug/switch
- `sensor.temperature_living` — a temperature sensor
- `lock.front_door` — a smart lock
- `climate.thermostat` — a thermostat

## Requirements
- Home Assistant instance with Matter integration enabled
- Long-lived access token from Home Assistant
- Matter devices commissioned in Home Assistant