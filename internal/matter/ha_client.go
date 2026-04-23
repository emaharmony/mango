package matter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// HomeAssistantClient connects to Home Assistant's websocket API
// and provides entity state and service call access for Matter devices.
type HomeAssistantClient struct {
	url       string
	token     string
	conn      *websocket.Conn
	mu        sync.Mutex
_states   map[string]Entity
	stateCh   chan StateEvent
	done      chan struct{}
	seq       int
}

// Entity represents a Home Assistant entity (device state).
type Entity struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes"`
	LastChanged time.Time              `json:"last_changed"`
	LastUpdated time.Time              `json:"last_updated"`
}

// StateEvent is emitted when an entity state changes.
type StateEvent struct {
	EntityID string `json:"entity_id"`
	OldState *Entity `json:"old_state,omitempty"`
	NewState *Entity `json:"new_state,omitempty"`
}

// ServiceCall represents a call to a Home Assistant service.
type ServiceCall struct {
	Domain     string                 `json:"domain"`
	Service    string                 `json:"service"`
	EntityID   string                 `json:"entity_id,omitempty"`
	ServiceData map[string]interface{} `json:"service_data,omitempty"`
}

// NewHomeAssistantClient creates a new HA websocket client.
func NewHomeAssistantClient(url, token string) *HomeAssistantClient {
	return &HomeAssistantClient{
		url:     url,
		token:   token,
		_states: make(map[string]Entity),
		stateCh: make(chan StateEvent, 100),
		done:    make(chan struct{}),
	}
}

// Connect opens the websocket connection and authenticates.
func (c *HomeAssistantClient) Connect(ctx context.Context) error {
	wsURL := c.url
	// Convert http(s) URL to ws(s)
	if len(wsURL) > 5 && wsURL[:5] == "https" {
		wsURL = "wss" + wsURL[5:]
	} else if len(wsURL) > 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:]
	}
	wsURL += "/api/websocket"

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial %s: %w", wsURL, err)
	}
	c.conn = conn

	// HA sends auth_required message first
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read auth_required: %w", err)
	}
	var authReq struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg, &authReq); err != nil || authReq.Type != "auth_required" {
		return fmt.Errorf("expected auth_required, got: %s", string(msg))
	}

	// Send auth
	authMsg, _ := json.Marshal(map[string]string{
		"type":        "auth",
		"access_token": c.token,
	})
	if err := conn.WriteMessage(websocket.TextMessage, authMsg); err != nil {
		return fmt.Errorf("write auth: %w", err)
	}

	// Read auth_ok
	_, msg, err = conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read auth_ok: %w", err)
	}
	var authResp struct {
		Type    string `json:"type"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(msg, &authResp); err != nil {
		return fmt.Errorf("parse auth response: %w", err)
	}
	if authResp.Type != "auth_ok" {
		return fmt.Errorf("auth failed: %s", authResp.Message)
	}

	log.Printf("matter: authenticated with Home Assistant at %s", c.url)
	return nil
}

// SubscribeEvents subscribes to state_changed events and starts listening.
func (c *HomeAssistantClient) SubscribeEvents(ctx context.Context) error {
	c.mu.Lock()
	c.seq++
	id := c.seq
	c.mu.Unlock()

	msg, _ := json.Marshal(map[string]interface{}{
		"id":        id,
		"type":      "subscribe_events",
		"event_type": "state_changed",
	})
	if err := connWrite(c, msg); err != nil {
		return fmt.Errorf("subscribe_events: %w", err)
	}

	go c.listenLoop(ctx)
	return nil
}

// GetStates fetches all current entity states from HA.
func (c *HomeAssistantClient) GetStates(ctx context.Context) (map[string]Entity, error) {
	c.mu.Lock()
	c.seq++
	id := c.seq
	c.mu.Unlock()

	msg, _ := json.Marshal(map[string]interface{}{
		"id":   id,
		"type": "get_states",
	})
	if err := connWrite(c, msg); err != nil {
		return nil, fmt.Errorf("get_states: %w", err)
	}

	_, resp, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read get_states response: %w", err)
	}

	var result struct {
		ID     int      `json:"id"`
		Type   string   `json:"type"`
		Success bool    `json:"success"`
		Result []Entity `json:"result,omitempty"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse get_states: %w", err)
	}

	states := make(map[string]Entity)
	for _, e := range result.Result {
		states[e.EntityID] = e
	}
	c.mu.Lock()
	c._states = states
	c.mu.Unlock()

	log.Printf("matter: loaded %d entities from Home Assistant", len(states))
	return states, nil
}

// CallService calls a Home Assistant service (e.g., turn_on, turn_off).
func (c *HomeAssistantClient) CallService(ctx context.Context, call ServiceCall) error {
	c.mu.Lock()
	c.seq++
	id := c.seq
	c.mu.Unlock()

	msg, _ := json.Marshal(map[string]interface{}{
		"id":           id,
		"type":         "call_service",
		"domain":       call.Domain,
		"service":      call.Service,
		"service_data": mergeEntityID(call.EntityID, call.ServiceData),
	})
	if err := connWrite(c, msg); err != nil {
		return fmt.Errorf("call_service %s/%s: %w", call.Domain, call.Service, err)
	}

	// Read response
	_, resp, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read call_service response: %w", err)
	}

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(resp, &result); err == nil && !result.Success {
		return fmt.Errorf("call_service %s/%s failed", call.Domain, call.Service)
	}

	log.Printf("matter: called service %s.%s on %s", call.Domain, call.Service, call.EntityID)
	return nil
}

// StateChanges returns a channel of state change events.
func (c *HomeAssistantClient) StateChanges() <-chan StateEvent {
	return c.stateCh
}

// GetEntity returns the cached state of an entity.
func (c *HomeAssistantClient) GetEntity(entityID string) (Entity, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c._states[entityID]
	return e, ok
}

// AllEntities returns all cached entity states.
func (c *HomeAssistantClient) AllEntities() map[string]Entity {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]Entity, len(c._states))
	for k, v := range c._states {
		result[k] = v
	}
	return result
}

// Close shuts down the client.
func (c *HomeAssistantClient) Close() error {
	close(c.done)
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// listenLoop reads messages from the websocket and dispatches state changes.
func (c *HomeAssistantClient) listenLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("matter: websocket read error: %v", err)
			return
		}

		var event struct {
			Type  string `json:"type"`
			Event struct {
				Data struct {
					EntityID  string   `json:"entity_id"`
					OldState  *Entity  `json:"old_state"`
					NewState  *Entity  `json:"new_state"`
				} `json:"data"`
			} `json:"event"`
		}
		if err := json.Unmarshal(msg, &event); err != nil {
			continue
		}
		if event.Type != "event" || event.Event.Data.EntityID == "" {
			continue
		}

		// Update cache
		if event.Event.Data.NewState != nil {
			c.mu.Lock()
			c._states[event.Event.Data.EntityID] = *event.Event.Data.NewState
			c.mu.Unlock()
		}

		// Notify subscribers
		select {
		case c.stateCh <- StateEvent{
			EntityID: event.Event.Data.EntityID,
			OldState: event.Event.Data.OldState,
			NewState: event.Event.Data.NewState,
		}:
		default:
			log.Printf("matter: state channel full, dropping event for %s", event.Event.Data.EntityID)
		}
	}
}

func connWrite(c *HomeAssistantClient, msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, msg)
}

func mergeEntityID(entityID string, data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		result[k] = v
	}
	if entityID != "" {
		result["entity_id"] = entityID
	}
	return result
}