package matter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMatterToolList(t *testing.T) {
	bot := &Bot{
		client: &HomeAssistantClient{
			_states: map[string]Entity{
				"light.living_room": {EntityID: "light.living_room", State: "on", Attributes: map[string]interface{}{"friendly_name": "Living Room Light"}},
				"switch.kitchen":    {EntityID: "switch.kitchen", State: "off", Attributes: map[string]interface{}{"friendly_name": "Kitchen Switch"}},
			},
		},
		entityFilters: []string{"light", "switch"},
	}
	tool := NewTool(bot)
	result, err := tool.Execute(context.Background(), "list")
	if err != nil {
		t.Fatalf("Execute list: %v", err)
	}
	if !strings.Contains(result, "light.living_room") {
		t.Errorf("list result missing light.living_room: %s", result)
	}
	if !strings.Contains(result, "2 Matter devices") {
		t.Errorf("list result missing device count: %s", result)
	}
}

func TestMatterToolState(t *testing.T) {
	bot := &Bot{
		client: &HomeAssistantClient{
			_states: map[string]Entity{
				"light.living_room": {EntityID: "light.living_room", State: "on", Attributes: map[string]interface{}{
					"friendly_name": "Living Room Light",
					"brightness":    255,
				}},
			},
		},
		entityFilters: []string{"light"},
	}
	tool := NewTool(bot)
	result, err := tool.Execute(context.Background(), "state light.living_room")
	if err != nil {
		t.Fatalf("Execute state: %v", err)
	}
	if !strings.Contains(result, "on") {
		t.Errorf("state result missing 'on': %s", result)
	}
	if !strings.Contains(result, "brightness") {
		t.Errorf("state result missing brightness: %s", result)
	}
}

func TestMatterToolUnknownCommand(t *testing.T) {
	bot := &Bot{
		client: &HomeAssistantClient{_states: map[string]Entity{}},
		entityFilters: []string{"light"},
	}
	tool := NewTool(bot)
	_, err := tool.Execute(context.Background(), "explode light.living_room")
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

// Mock HA server for integration-style tests
func TestHAClientAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/websocket" {
			// Upgrade to websocket would require a real ws server
			// For now, test the auth message construction
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	// This test just verifies the client can be created
	client := NewHomeAssistantClient(server.URL, "test-token")
	if client == nil {
		t.Fatal("client is nil")
	}
}

// TestMergeEntityID
func TestMergeEntityID(t *testing.T) {
	result := mergeEntityID("light.living", map[string]interface{}{"brightness": 128})
	if result["entity_id"] != "light.living" {
		t.Errorf("expected entity_id=light.living, got %v", result["entity_id"])
	}
	if result["brightness"] != 128 {
		t.Errorf("expected brightness=128, got %v", result["brightness"])
	}
}