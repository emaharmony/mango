package matter

import (
	"context"
	"testing"
)

func TestDomainFromEntity(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"light.living_room", "light"},
		{"switch.kitchen_outlet", "switch"},
		{"sensor.temperature_living", "sensor"},
		{"lock.front_door", "lock"},
		{"climate.thermostat", "climate"},
		{"nodot", "nodot"},
	}
	for _, tt := range tests {
		got := domainFromEntity(tt.input)
		if got != tt.expected {
			t.Errorf("domainFromEntity(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsWatched(t *testing.T) {
	bot := &Bot{
		entityFilters: []string{"light", "switch", "sensor"},
	}
	tests := []struct {
		entityID string
		want    bool
	}{
		{"light.living_room", true},
		{"switch.kitchen", true},
		{"sensor.temp", true},
		{"climate.thermostat", false},
		{"lock.front_door", false},
	}
	for _, tt := range tests {
		got := bot.isWatched(tt.entityID)
		if got != tt.want {
			t.Errorf("isWatched(%q) = %v, want %v", tt.entityID, got, tt.want)
		}
	}
}

func TestControllerConfigDefaults(t *testing.T) {
	ctrl := NewController(ControllerConfig{})
	if ctrl.cfg.NodePath != "node" {
		t.Errorf("expected default NodePath 'node', got %q", ctrl.cfg.NodePath)
	}
	if ctrl.cfg.Port != 5540 {
		t.Errorf("expected default Port 5540, got %d", ctrl.cfg.Port)
	}
}

func TestControllerIsRunning(t *testing.T) {
	ctrl := NewController(ControllerConfig{StorageDir: t.TempDir()})
	if ctrl.IsRunning() {
		t.Error("expected controller not running before Start()")
	}
}

func TestFindControllerScript(t *testing.T) {
	// This test just ensures the function doesn't panic
	// In CI/test env, there's no script file, so it returns ""
	result := findControllerScript()
	_ = result // may be empty in test env
}

func TestMergeEntityID(t *testing.T) {
	result := mergeEntityID("light.living", map[string]interface{}{"brightness": 128})
	if result["entity_id"] != "light.living" {
		t.Errorf("expected entity_id=light.living, got %v", result["entity_id"])
	}
	if result["brightness"] != 128 {
		t.Errorf("expected brightness=128, got %v", result["brightness"])
	}
}

func TestDeviceCommand(t *testing.T) {
	cmd := DeviceCommand{
		EntityID: "light.living_room",
		Command:  "on",
		Params:   map[string]interface{}{"brightness": 255},
	}
	if cmd.EntityID != "light.living_room" {
		t.Errorf("expected entity_id=light.living_room, got %q", cmd.EntityID)
	}
	if cmd.Command != "on" {
		t.Errorf("expected command=on, got %q", cmd.Command)
	}
}

func TestBotConfigDefaults(t *testing.T) {
	bot, err := NewBot(BotConfig{}, nil)
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}
	// Should have default entity filters
	if len(bot.entityFilters) == 0 {
		t.Error("expected default entity filters")
	}
	// Should have default agent bindings
	if len(bot.agentBindings) == 0 {
		t.Error("expected default agent bindings")
	}
	// Verify some specific defaults
	found := false
	for _, f := range bot.entityFilters {
		if f == "light" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'light' in default entity filters")
	}
}

func TestMatterToolUnknownCommand(t *testing.T) {
	ctrl := NewController(ControllerConfig{StorageDir: t.TempDir()})
	bot := &Bot{
		controller:    ctrl,
		entityFilters: []string{"light"},
	}
	tool := NewTool(bot)
	_, err := tool.Execute(context.Background(), "explode light.living_room")
	if err == nil {
		t.Error("expected error for unknown command")
	}
}