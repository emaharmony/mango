package matter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/carlosmaranje/mango/internal/orchestrator"
)

// Bot is the Matter channel gateway — similar pattern to discord.Bot.
// It connects to Home Assistant, watches Matter device state changes,
// and dispatches agent tasks based on device events.
type Bot struct {
	client     *HomeAssistantClient
	dispatcher *orchestrator.Dispatcher
	entityFilters []string // e.g., "light.", "switch.", "sensor.", "lock."
	watchCh    <-chan StateEvent
	mu         sync.Mutex
	agentBindings map[string]string // entity domain -> agent name
}

// BotConfig holds Matter channel configuration.
type BotConfig struct {
	URL           string            `yaml:"url"`
	Token         string            `yaml:"token"`
	EntityFilters []string          `yaml:"entity_filters,omitempty"` // domains to watch, e.g. ["light", "switch", "sensor"]
	AgentBindings map[string]string `yaml:"agent_bindings,omitempty"` // domain -> agent, e.g. {"light": "jirby"}
}

// NewBot creates a new Matter channel bot.
func NewBot(cfg BotConfig, dispatcher *orchestrator.Dispatcher) (*Bot, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("matter: home_assistant_url is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("matter: token is required")
	}

	filters := cfg.EntityFilters
	if len(filters) == 0 {
		// Default: watch common Matter device domains
		filters = []string{"light", "switch", "sensor", "lock", "climate", "cover", "fan", "humidifier"}
	}

	bindings := cfg.AgentBindings
	if len(bindings) == 0 {
		bindings = map[string]string{
			"light":   "worker",
			"switch":  "worker",
			"sensor":  "worker",
			"lock":    "worker",
			"climate": "worker",
		}
	}

	return &Bot{
		client:        NewHomeAssistantClient(cfg.URL, cfg.Token),
		dispatcher:    dispatcher,
		entityFilters: filters,
		agentBindings: bindings,
	}, nil
}

// Start connects to Home Assistant, fetches initial state, and subscribes to events.
func (b *Bot) Start(ctx context.Context) error {
	if err := b.client.Connect(ctx); err != nil {
		return fmt.Errorf("matter: connect: %w", err)
	}

	// Fetch initial state
	states, err := b.client.GetStates(ctx)
	if err != nil {
		return fmt.Errorf("matter: get states: %w", err)
	}

	// Log Matter devices
	matterCount := 0
	for id, entity := range states {
		if b.isWatched(id) {
			matterCount++
			log.Printf("matter: device %s = %s", id, entity.State)
		}
	}
	log.Printf("matter: watching %d devices across %d domains", matterCount, len(b.entityFilters))

	// Subscribe to state changes
	if err := b.client.SubscribeEvents(ctx); err != nil {
		return fmt.Errorf("matter: subscribe: %w", err)
	}

	b.watchCh = b.client.StateChanges()

	// Start event loop
	go b.eventLoop(ctx)

	log.Printf("matter: channel gateway active")
	return nil
}

// Close shuts down the Matter channel.
func (b *Bot) Close() error {
	log.Printf("matter: shutting down")
	return b.client.Close()
}

// TurnOn turns on a device via Home Assistant.
func (b *Bot) TurnOn(ctx context.Context, entityID string) error {
	domain := domainFromEntity(entityID)
	return b.client.CallService(ctx, ServiceCall{
		Domain:   domain,
		Service:  "turn_on",
		EntityID: entityID,
	})
}

// TurnOff turns off a device via Home Assistant.
func (b *Bot) TurnOff(ctx context.Context, entityID string) error {
	domain := domainFromEntity(entityID)
	return b.client.CallService(ctx, ServiceCall{
		Domain:   domain,
		Service:  "turn_off",
		EntityID: entityID,
	})
}

// Toggle toggles a device.
func (b *Bot) Toggle(ctx context.Context, entityID string) error {
	domain := domainFromEntity(entityID)
	return b.client.CallService(ctx, ServiceCall{
		Domain:   domain,
		Service:  "toggle",
		EntityID: entityID,
	})
}

// SetBrightness sets a light's brightness (0-255).
func (b *Bot) SetBrightness(ctx context.Context, entityID string, brightness int) error {
	return b.client.CallService(ctx, ServiceCall{
		Domain:   "light",
		Service:  "turn_on",
		EntityID: entityID,
		ServiceData: map[string]interface{}{
			"brightness": brightness,
		},
	})
}

// SetTemperature sets a climate device's temperature.
func (b *Bot) SetTemperature(ctx context.Context, entityID string, temp float64) error {
	return b.client.CallService(ctx, ServiceCall{
		Domain:   "climate",
		Service:  "set_temperature",
		EntityID: entityID,
		ServiceData: map[string]interface{}{
			"temperature": temp,
		},
	})
}

// GetDevices returns all watched Matter devices as map[string]interface{} (gateway.MatterProvider).
func (b *Bot) GetDevices() map[string]interface{} {
	devices := b.getDevicesInternal()
	result := make(map[string]interface{}, len(devices))
	for k, v := range devices {
		result[k] = v
	}
	return result
}

// GetState returns a single entity as interface{} (gateway.MatterProvider).
func (b *Bot) GetState(entityID string) (interface{}, bool) {
	e, ok := b.client.GetEntity(entityID)
	if !ok {
		return nil, false
	}
	return e, true
}

// GetStateTyped returns the cached typed state of an entity.
func (b *Bot) GetStateTyped(entityID string) (Entity, bool) {
	return b.client.GetEntity(entityID)
}

// getDevicesInternal returns all watched Matter devices with typed state.
func (b *Bot) getDevicesInternal() map[string]Entity {
	all := b.client.AllEntities()
	result := make(map[string]Entity)
	for id, e := range all {
		if b.isWatched(id) {
			result[id] = e
		}
	}
	return result
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

	// Build a natural language description of the state change
	desc := fmt.Sprintf("Matter device %s changed state", event.EntityID)
	if event.NewState != nil {
		desc = fmt.Sprintf("Matter device %s is now %s", event.EntityID, event.NewState.State)
	}

	log.Printf("matter: %s -> dispatching to agent %q", desc, agentName)

	// Dispatch to the appropriate agent via the dispatcher
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

// domainFromEntity extracts the domain from an entity ID (e.g., "light.living_room" -> "light").
func domainFromEntity(entityID string) string {
	parts := strings.SplitN(entityID, ".", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return entityID
}