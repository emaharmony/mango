package matter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Controller manages a native Matter controller via an embedded Node.js
// subprocess (matter.js). This replaces the Home Assistant dependency —
// users commission and control Matter devices entirely within Mango.
//
// The Node.js subprocess runs a thin controller script that:
//   - Creates a Matter fabric (commissioner)
//   - Listens for commissioning requests (QR code / manual pairing)
//   - Discovers and controls devices on the local network
//   - Exposes device state + control via stdout/stderr JSON messages
//
// Architecture:
//
//	Matter Devices → Thread/WiFi → Mango (matter.js subprocess) → internal/matter channel

const (
	// ControllerScriptName is the embedded JS controller filename.
	ControllerScriptName = "matter-controller.mjs"

	// DefaultNodePath is the default node binary.
	DefaultNodePath = "node"

	// CommissionTimeout is how long to wait for a device commissioning.
	CommissionTimeout = 120 * time.Second
)

// ControllerConfig holds native Matter controller configuration.
type ControllerConfig struct {
	Enabled       bool   `yaml:"enabled,omitempty"`
	NodePath      string `yaml:"node_path,omitempty"`       // Path to node binary (default: "node")
	StorageDir    string `yaml:"storage_dir,omitempty"`     // Where Matter fabric data lives
	NetworkInterface string `yaml:"network_interface,omitempty"` // Bind to specific NIC (optional)
	Port          int    `yaml:"port,omitempty"`             // Matter port (default: 5540)
}

// Controller manages the matter.js subprocess.
type Controller struct {
	cfg    ControllerConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	_states map[string]Entity
	stateCh chan StateEvent
	done    chan struct{}
	running bool
}

// ControllerMessage is a JSON message from the Node.js subprocess.
type ControllerMessage struct {
	Type string          `json:"type"` // "state", "device", "log", "error"
	Data json.RawMessage `json:"data"`
}

// DeviceStateMessage is a state update from the controller.
type DeviceStateMessage struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes"`
}

// CommissionRequest asks the controller to commission a new device.
type CommissionRequest struct {
	Code string `json:"code"` // QR code or manual pairing code
}

// DeviceCommand sends a command to a specific device.
type DeviceCommand struct {
	EntityID    string                 `json:"entity_id"`
	Command     string                 `json:"command"` // "on", "off", "toggle", "set_brightness", "set_temperature"
	Params      map[string]interface{} `json:"params,omitempty"`
}

// NewController creates a new native Matter controller.
func NewController(cfg ControllerConfig) *Controller {
	if cfg.NodePath == "" {
		cfg.NodePath = DefaultNodePath
	}
	if cfg.StorageDir == "" {
		home, _ := os.UserHomeDir()
		cfg.StorageDir = filepath.Join(home, ".mango", "matter")
	}
	if cfg.Port == 0 {
		cfg.Port = 5540
	}
	return &Controller{
		cfg:     cfg,
		_states: make(map[string]Entity),
		stateCh: make(chan StateEvent, 100),
		done:    make(chan struct{}),
	}
}

// Start launches the matter.js controller subprocess.
func (c *Controller) Start(ctx context.Context) error {
	scriptPath := findControllerScript()
	if scriptPath == "" {
		return fmt.Errorf("matter: controller script not found — run 'mango matter setup' first")
	}

	// Ensure storage directory exists
	if err := os.MkdirAll(c.cfg.StorageDir, 0o755); err != nil {
		return fmt.Errorf("matter: create storage dir: %w", err)
	}

	args := []string{scriptPath,
		"--storage", c.cfg.StorageDir,
		"--port", fmt.Sprintf("%d", c.cfg.Port),
	}
	if c.cfg.NetworkInterface != "" {
		args = append(args, "--interface", c.cfg.NetworkInterface)
	}

	c.cmd = exec.CommandContext(ctx, c.cfg.NodePath, args...)
	c.cmd.Env = append(os.Environ(),
		"MATTER_STORAGE_DIR="+c.cfg.StorageDir,
	)

	// Wire stdin/stdout for JSON message passing
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("matter: stdin pipe: %w", err)
	}
	c.stdin = stdin

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("matter: stdout pipe: %w", err)
	}
	c.stdout = bufio.NewScanner(stdout)

	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("matter: stderr pipe: %w", err)
	}

	// Log stderr (controller debug output)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Printf("matter-controller: %s", sc.Text())
		}
	}()

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("matter: start controller: %w", err)
	}

	c.running = true
	go c.readLoop(ctx)
	go c.waitLoop()

	log.Printf("matter: native controller started (pid=%d, storage=%s, port=%d)", c.cmd.Process.Pid, c.cfg.StorageDir, c.cfg.Port)
	return nil
}

// Close shuts down the controller.
func (c *Controller) Close() error {
	close(c.done)
	c.running = false
	if c.stdin != nil {
		// Send shutdown command
		json.NewEncoder(c.stdin).Encode(map[string]string{"type": "shutdown"})
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		// Give it a moment to shut down gracefully
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			c.cmd.Process.Kill()
		}
	}
	return nil
}

// Commission adds a new Matter device using a QR code or manual pairing code.
func (c *Controller) Commission(ctx context.Context, code string) error {
	msg := map[string]interface{}{
		"type": "commission",
		"code": code,
	}
	return c.sendCommand(msg)
}

// SendDeviceCommand sends a control command to a device.
func (c *Controller) SendDeviceCommand(ctx context.Context, cmd DeviceCommand) error {
	msg := map[string]interface{}{
		"type":      "command",
		"entity_id": cmd.EntityID,
		"command":   cmd.Command,
		"params":    cmd.Params,
	}
	return c.sendCommand(msg)
}

// GetDevices returns all known Matter devices.
func (c *Controller) GetDevices() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]interface{}, len(c._states))
	for k, v := range c._states {
		result[k] = v
	}
	return result
}

// GetState returns a specific device's state.
func (c *Controller) GetState(entityID string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c._states[entityID]
	return e, ok
}

// StateChanges returns the channel of state change events.
func (c *Controller) StateChanges() <-chan StateEvent {
	return c.stateCh
}

// IsRunning returns whether the controller subprocess is active.
func (c *Controller) IsRunning() bool {
	return c.running
}

// sendCommand sends a JSON message to the controller subprocess via stdin.
func (c *Controller) sendCommand(msg interface{}) error {
	if c.stdin == nil {
		return fmt.Errorf("matter: controller not running")
	}
	return json.NewEncoder(c.stdin).Encode(msg)
}

// readLoop reads JSON messages from the controller subprocess stdout.
func (c *Controller) readLoop(ctx context.Context) {
	for c.stdout.Scan() {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		line := c.stdout.Bytes()
		var msg ControllerMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("matter: parse controller message: %v (line: %s)", err, string(line))
			continue
		}

		switch msg.Type {
		case "state":
			var dev DeviceStateMessage
			if err := json.Unmarshal(msg.Data, &dev); err != nil {
				log.Printf("matter: parse device state: %v", err)
				continue
			}
			c.updateState(dev)
		case "log":
			log.Printf("matter-controller: %s", string(msg.Data))
		case "error":
			log.Printf("matter-controller error: %s", string(msg.Data))
		}
	}
	if err := c.stdout.Err(); err != nil {
		log.Printf("matter: controller stdout error: %v", err)
	}
}

// updateState processes a device state update from the controller.
func (c *Controller) updateState(dev DeviceStateMessage) {
	entity := Entity{
		EntityID:   dev.EntityID,
		State:      dev.State,
		Attributes: dev.Attributes,
	}

	c.mu.Lock()
	old, hadOld := c._states[dev.EntityID]
	c._states[dev.EntityID] = entity
	c.mu.Unlock()

	// Emit state change event
	event := StateEvent{
		EntityID: dev.EntityID,
		NewState: &entity,
	}
	if hadOld {
		event.OldState = &old
	}

	select {
	case c.stateCh <- event:
	default:
		log.Printf("matter: state channel full, dropping event for %s", dev.EntityID)
	}
}

// waitLoop waits for the subprocess to exit.
func (c *Controller) waitLoop() {
	if c.cmd != nil {
		err := c.cmd.Wait()
		c.running = false
		if err != nil {
			log.Printf("matter: controller exited with error: %v", err)
		} else {
			log.Printf("matter: controller exited cleanly")
		}
	}
}

// findControllerScript locates the matter-controller.mjs script.
// It searches: ./config/matter/, ./internal/matter/scripts/, and the binary's directory.
func findControllerScript() string {
	searchPaths := []string{
		"config/matter/" + ControllerScriptName,
		"internal/matter/scripts/" + ControllerScriptName,
	}

	// Also check relative to the executable
	if exe, err := os.Executable(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(filepath.Dir(exe), "matter", ControllerScriptName))
	}

	for _, p := range searchPaths {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}