package matter

import (
	"context"
	"fmt"
	"strings"

	"github.com/carlosmaranje/mango/internal/tools"
)

// Tool provides Matter/IoT device control as an agent tool.
// Agents can call these methods to interact with smart devices.
type Tool struct {
	bot *Bot
}

// NewTool creates a Matter tool bound to a Bot.
func NewTool(bot *Bot) *Tool {
	return &Tool{bot: bot}
}

// Name returns the tool name.
func (t *Tool) Name() string {
	return "matter"
}

// Description returns a human-readable description.
func (t *Tool) Description() string {
	return "Control Matter/IoT devices via Home Assistant. Commands: list, state <entity_id>, on <entity_id>, off <entity_id>, toggle <entity_id>, brightness <entity_id> <0-255>, temp <entity_id> <value>"
}

// Parameters returns tool parameter definitions.
func (t *Tool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "command", Type: "string", Description: "One of: list, state, on, off, toggle, brightness, temp", Required: true},
		{Name: "entity_id", Type: "string", Description: "Home Assistant entity ID (e.g., light.living_room)", Required: false},
		{Name: "value", Type: "string", Description: "Value for brightness (0-255) or temperature", Required: false},
	}
}

// Returns describes the tool's return format.
func (t *Tool) Returns() string {
	return "A human-readable result string describing device state or action outcome"
}

// Execute runs a Matter tool command.
func (t *Tool) Execute(ctx context.Context, input string) (string, error) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return t.listDevices()
	}

	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "list":
		return t.listDevices()

	case "state":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: state <entity_id>")
		}
		return t.getDeviceState(parts[1])

	case "on":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: on <entity_id>")
		}
		if err := t.bot.TurnOn(ctx, parts[1]); err != nil {
			return "", fmt.Errorf("turn on %s: %w", parts[1], err)
		}
		return fmt.Sprintf("✅ Turned on %s", parts[1]), nil

	case "off":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: off <entity_id>")
		}
		if err := t.bot.TurnOff(ctx, parts[1]); err != nil {
			return "", fmt.Errorf("turn off %s: %w", parts[1], err)
		}
		return fmt.Sprintf("✅ Turned off %s", parts[1]), nil

	case "toggle":
		if len(parts) < 2 {
			return "", fmt.Errorf("usage: toggle <entity_id>")
		}
		if err := t.bot.Toggle(ctx, parts[1]); err != nil {
			return "", fmt.Errorf("toggle %s: %w", parts[1], err)
		}
		return fmt.Sprintf("✅ Toggled %s", parts[1]), nil

	case "brightness":
		if len(parts) < 3 {
			return "", fmt.Errorf("usage: brightness <entity_id> <0-255>")
		}
		var brightness int
		if _, err := fmt.Sscanf(parts[2], "%d", &brightness); err != nil {
			return "", fmt.Errorf("invalid brightness value: %s", parts[2])
		}
		if err := t.bot.SetBrightness(ctx, parts[1], brightness); err != nil {
			return "", fmt.Errorf("set brightness %s: %w", parts[1], err)
		}
		return fmt.Sprintf("✅ Set %s brightness to %d", parts[1], brightness), nil

	case "temp", "temperature":
		if len(parts) < 3 {
			return "", fmt.Errorf("usage: temp <entity_id> <value>")
		}
		var temp float64
		if _, err := fmt.Sscanf(parts[2], "%f", &temp); err != nil {
			return "", fmt.Errorf("invalid temperature value: %s", parts[2])
		}
		if err := t.bot.SetTemperature(ctx, parts[1], temp); err != nil {
			return "", fmt.Errorf("set temperature %s: %w", parts[1], err)
		}
		return fmt.Sprintf("✅ Set %s temperature to %.1f", parts[1], temp), nil

	default:
		return "", fmt.Errorf("unknown command: %s. Commands: list, state, on, off, toggle, brightness, temp", cmd)
	}
}

// listDevices returns a formatted list of all watched Matter devices.
func (t *Tool) listDevices() (string, error) {
	devicesRaw := t.bot.GetDevices()
	if len(devicesRaw) == 0 {
		return "No Matter devices found.", nil
	}

	// Convert to typed for display
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🏠 %d Matter devices:\n\n", len(devicesRaw)))

	for id, raw := range devicesRaw {
		entity, ok := raw.(Entity)
		if !ok {
			sb.WriteString(fmt.Sprintf("- **%s**: (unknown type)\n", id))
			continue
		}
		friendlyName := ""
		if name, ok := entity.Attributes["friendly_name"].(string); ok {
			friendlyName = name
		}
		sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", id, friendlyName, entity.State))
	}

	return sb.String(), nil
}

// getDeviceState returns the state of a specific device.
func (t *Tool) getDeviceState(entityID string) (string, error) {
	entityRaw, ok := t.bot.GetState(entityID)
	if !ok {
		return "", fmt.Errorf("entity %s not found", entityID)
	}
	entity, ok := entityRaw.(Entity)
	if !ok {
		return "", fmt.Errorf("entity %s has unexpected type", entityID)
	}

	friendlyName := ""
	if name, ok := entity.Attributes["friendly_name"].(string); ok {
		friendlyName = name
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📱 **%s** (%s)\n", entityID, friendlyName))
	sb.WriteString(fmt.Sprintf("State: **%s**\n", entity.State))

	// Show key attributes
	for _, key := range []string{"brightness", "color_temp", "temperature", "humidity", "current_temperature", "battery", "unit_of_measurement"} {
		if val, ok := entity.Attributes[key]; ok {
			sb.WriteString(fmt.Sprintf("%s: %v\n", key, val))
		}
	}

	return sb.String(), nil
}