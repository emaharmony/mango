package matter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/carlosmaranje/mango/internal/orchestrator"
)

// Bot is the Matter channel gateway — manages native Matter devices
// via an embedded matter.js controller subprocess.
type Bot struct {
	controller    *Controller
	dispatcher    *orchestrator.Dispatcher
	entityFilters []string
	agentBindings map[string]string
	mu            sync.Mutex
	watchCh       <-chan StateEvent
}

// BotConfig holds Matter channel configuration.
type BotConfig struct {
	Enabled        bool              `yaml:"enabled,omitempty"`
	Controller     ControllerConfig  `yaml:",inline"`
	EntityFilters  []string          `yaml:"entity_filters,omitempty"`
	AgentBindings  map[string]string `yaml:"agent_bindings,omitempty"`

	// Deprecated: Home Assistant mode (will be removed in future release)
	HomeAssistantURL   string `yaml:"home_assistant_url,omitempty"`
	HomeAssistantToken string `yaml:"home_assistant_token,omitempty"`
}

// NewBot creates a new Matter channel bot with the native controller.
func NewBot(cfg BotConfig, dispatcher *orchestrator.Dispatcher) (*Bot, error) {
	filters := cfg.EntityFilters
	if len(filters) == 0 {
		filters = []string{"light", "switch", "sensor", "lock", "climate", "cover", "fan", "humidifier", "device"}
	}

	bindings := cfg.AgentBindings
	if len(bindings) == 0 {
		bindings = map[string]string{
			"light":   "worker",
			"switch":  "worker",
			"sensor":  "worker",
			"lock":    "worker",
			"climate": "worker",
			"device":  "worker",
		}
	}

	controller := NewController(cfg.Controller)

	return &Bot{
		controller:    controller,
		dispatcher:    dispatcher,
		entityFilters: filters,
		agentBindings: bindings,
	}, nil
}

// Start launches the native Matter controller and begins watching devices.
func (b *Bot) Start(ctx context.Context) error {
	if err := b.controller.Start(ctx); err != nil {
		return fmt.Errorf("matter: start controller: %w", err)
	}

	b.watchCh = b.controller.StateChanges()

	// Log current devices
	devices := b.controller.GetDevices()
	matterCount := 0
	for id := range devices {
		if b.isWatched(id) {
			matterCount++
		}
	}
	log.Printf("matter: native controller active — %d device(s) across %d domains", matterCount, len(b.entityFilters))

	// Start event loop
	go b.eventLoop(ctx)

	return nil
}

// Close shuts down the Matter controller.
func (b *Bot) Close() error {
	log.Printf("matter: shutting down native controller")
	return b.controller.Close()
}

// Commission adds a new Matter device using a QR or manual pairing code.
func (b *Bot) Commission(ctx context.Context, code string) error {
	return b.controller.Commission(ctx, code)
}

// TurnOn turns on a device.
func (b *Bot) TurnOn(ctx context.Context, entityID string) error {
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  "on",
	})
}

// TurnOff turns off a device.
func (b *Bot) TurnOff(ctx context.Context, entityID string) error {
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  "off",
	})
}

// Toggle toggles a device.
func (b *Bot) Toggle(ctx context.Context, entityID string) error {
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  "toggle",
	})
}

// SetBrightness sets a light's brightness (0-255).
func (b *Bot) SetBrightness(ctx context.Context, entityID string, brightness int) error {
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  "set_brightness",
		Params:   map[string]interface{}{"brightness": brightness},
	})
}

// SetTemperature sets a climate device's temperature.
func (b *Bot) SetTemperature(ctx context.Context, entityID string, temp float64) error {
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  "set_temperature",
		Params:   map[string]interface{}{"temperature": temp},
	})
}

// AddDevice registers a new entity for monitoring by adding its domain to the filter list.
func (b *Bot) AddDevice(ctx context.Context, entityID, friendlyName string) error {
	domain := domainFromEntity(entityID)
	b.mu.Lock()
	// Ensure domain is in entity filters
	found := false
	for _, f := range b.entityFilters {
		if f == domain {
			found = true
			break
		}
	}
	if !found {
		b.entityFilters = append(b.entityFilters, domain)
		log.Printf("matter: added domain %q to entity filters", domain)
	}
	b.mu.Unlock()

	// Request the controller to subscribe/watch this entity
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  "subscribe",
		Params:   map[string]interface{}{"friendly_name": friendlyName},
	})
}

// RemoveDevice removes an entity from monitoring.
func (b *Bot) RemoveDevice(ctx context.Context, entityID string) error {
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  "unsubscribe",
	})
}

// CallService sends an arbitrary service call to a device via the controller.
func (b *Bot) CallService(ctx context.Context, domain, service, entityID string, payload map[string]interface{}) error {
	return b.controller.SendDeviceCommand(ctx, DeviceCommand{
		EntityID: entityID,
		Command:  domain + "." + service,
		Params:   payload,
	})
}

// GetDevices returns all Matter devices (implements gateway.MatterProvider).
func (b *Bot) GetDevices() map[string]interface{} {
	return b.controller.GetDevices()
}

// GetState returns a specific device's state (implements gateway.MatterProvider).
func (b *Bot) GetState(entityID string) (interface{}, bool) {
	return b.controller.GetState(entityID)
}

// IsRunning reports whether the controller is active.
func (b *Bot) IsRunning() bool {
	return b.controller.IsRunning()
}

// eventLoop processes state change events and dispatches to agents.
func (b *Bot) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-b.watchCh:
			if !ok {
				return
			}
			if !b.isWatched(event.EntityID) {
				continue
			}
			b.handleStateChange(ctx, event)
		}
	}
}

// handleStateChange dispatches an agent task when a device state changes.
func (b *Bot) handleStateChange(ctx context.Context, event StateEvent) {
	domain := domainFromEntity(event.EntityID)

	agentName, ok := b.agentBindings[domain]
	if !ok {
		agentName = "worker"
	}

	desc := fmt.Sprintf("Matter device %s changed state", event.EntityID)
	if event.NewState != nil {
		desc = fmt.Sprintf("Matter device %s is now %s", event.EntityID, event.NewState.State)
	}

	log.Printf("matter: %s -> dispatching to agent %q", desc, agentName)

	task, err := b.dispatcher.Submit(ctx, desc, agentName)
	if err != nil {
		log.Printf("matter: dispatch error for %s: %v", event.EntityID, err)
	} else {
		log.Printf("matter: dispatched task %s for %s", task.ID, event.EntityID)
	}
}

// isWatched checks if an entity ID matches the configured domain filters.
func (b *Bot) isWatched(entityID string) bool {
	domain := domainFromEntity(entityID)
	for _, f := range b.entityFilters {
		if domain == f {
			return true
		}
	}
	return false
}

// domainFromEntity extracts the domain from an entity ID.
func domainFromEntity(entityID string) string {
	parts := strings.SplitN(entityID, ".", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return entityID
}