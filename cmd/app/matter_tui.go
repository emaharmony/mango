package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"text/tabwriter"
)

// matterModel is the Bubble Tea model for the Matter device dashboard TUI.
type matterModel struct {
	client   *gatewayClient
	styles   matterStyles
	devices  []matterDevice
	connected bool
	err      string
	quitting bool
	width    int
	height   int
	cursor   int
	offset   int
}

type matterDevice struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes"`
	LastChanged string                 `json:"last_changed"`
	LastUpdated string                 `json:"last_updated"`
}

type matterStyles struct {
	title      lipgloss.Style
	header     lipgloss.Style
	row        lipgloss.Style
	selected   lipgloss.Style
	online     lipgloss.Style
	offline    lipgloss.Style
	errStyle   lipgloss.Style
	help       lipgloss.Style
	border     lipgloss.Style
	dimmed     lipgloss.Style
}

func newMatterStyles() matterStyles {
	return matterStyles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFA500")),
		header:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#333333")),
		selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD700")),
		online:   lipgloss.NewStyle().Foreground(lipgloss.Color("#4CAF50")),
		offline:  lipgloss.NewStyle().Foreground(lipgloss.Color("#F44336")),
		errStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")),
		help:     lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
		border:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		dimmed:   lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")),
	}
}

type fetchMsg struct {
	devices map[string]interface{}
	connected bool
	err      error
}

func fetchDevices(client *gatewayClient) tea.Cmd {
	return func() tea.Msg {
		var resp struct {
			Connected bool                   `json:"connected"`
			Devices   map[string]interface{} `json:"devices"`
		}
		if err := client.request(context.Background(), "GET", "/matter", nil, &resp); err != nil {
			return fetchMsg{err: err}
		}
		return fetchMsg{devices: resp.Devices, connected: resp.Connected}
	}
}

func initialMatterModel(client *gatewayClient) matterModel {
	return matterModel{
		client: client,
		styles: newMatterStyles(),
	}
}

func (m matterModel) Init() tea.Cmd {
	return tea.Batch(fetchDevices(m.client), tea.EnterAltScreen)
}

func (m matterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset--
				}
			}
		case "down", "j":
			if m.cursor < len(m.devices)-1 {
				m.cursor++
				maxVisible := m.height - 8
				if maxVisible < 1 {
					maxVisible = 1
				}
				if m.cursor >= m.offset+maxVisible {
					m.offset++
				}
			}
		case "r":
			m.err = ""
			return m, fetchDevices(m.client)
		}

	case fetchMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			m.connected = false
		} else {
			m.connected = msg.connected
			m.err = ""
			m.devices = parseDevices(msg.devices)
			if m.cursor >= len(m.devices) {
				m.cursor = 0
			}
		}
		return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return fetchDevices(m.client)()
		})
	}

	return m, nil
}

func parseDevices(raw map[string]interface{}) []matterDevice {
	var devices []matterDevice
	for id, v := range raw {
		d := matterDevice{EntityID: id}
		// Try to parse the device fields from the interface{}
		if data, ok := v.(map[string]interface{}); ok {
			if s, ok := data["state"].(string); ok {
				d.State = s
			}
			if attrs, ok := data["attributes"].(map[string]interface{}); ok {
				d.Attributes = attrs
			}
			if lc, ok := data["last_changed"].(string); ok {
				d.LastChanged = lc
			}
			if lu, ok := data["last_updated"].(string); ok {
				d.LastUpdated = lu
			}
		} else {
			// If it's already a structured type (from JSON round-trip)
			b, _ := json.Marshal(v)
			json.Unmarshal(b, &d)
		}
		if d.State == "" {
			d.State = "unknown"
		}
		devices = append(devices, d)
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].EntityID < devices[j].EntityID
	})
	return devices
}

func (m matterModel) View() string {
	if m.quitting {
		return ""
	}

	w := m.width
	if w < 60 {
		w = 60
	}

	var b strings.Builder

	// Title bar
	title := m.styles.title.Render("🏠 Mango Matter Dashboard")
	b.WriteString(title)
	b.WriteString("\n")

	// Connection status
	connStatus := m.styles.online.Render("● Connected")
	if !m.connected {
		connStatus = m.styles.offline.Render("● Disconnected")
	}
	b.WriteString(fmt.Sprintf("%s  %d devices\n", connStatus, len(m.devices)))

	if m.err != "" {
		b.WriteString(m.styles.errStyle.Render(fmt.Sprintf("Error: %s", m.err)))
		b.WriteString("\n")
	}

	b.WriteString(strings.Repeat("─", min(w, 80)))
	b.WriteString("\n")

	// Header
	header := fmt.Sprintf("%-35s %-12s %s", "DEVICE", "STATE", "FRIENDLY NAME")
	b.WriteString(m.styles.header.Render(header))
	b.WriteString("\n")

	// Device list
	maxVisible := m.height - 8
	if maxVisible < 1 {
		maxVisible = 20
	}
	end := min(m.offset+maxVisible, len(m.devices))
	for i := m.offset; i < end; i++ {
		d := m.devices[i]
		friendlyName := ""
		if d.Attributes != nil {
			if fn, ok := d.Attributes["friendly_name"].(string); ok {
				friendlyName = fn
			}
		}

		stateStyle := m.styles.online
		if d.State == "off" || d.State == "unavailable" || d.State == "unknown" {
			stateStyle = m.styles.offline
		}

		row := fmt.Sprintf("%-35s %-12s %s", d.EntityID, stateStyle.Render(d.State), m.styles.dimmed.Render(friendlyName))
		if i == m.cursor {
			row = m.styles.selected.Render("▶ " + row)
		} else {
			row = "  " + row
		}
		b.WriteString(row)
		b.WriteString("\n")
	}

	if len(m.devices) == 0 && m.connected {
		b.WriteString(m.styles.dimmed.Render("  No Matter devices found. Ensure Home Assistant is running with Matter integration."))
		b.WriteString("\n")
	}

	b.WriteString(strings.Repeat("─", min(w, 80)))
	b.WriteString("\n")

	// Selected device detail
	if m.cursor < len(m.devices) && len(m.devices) > 0 {
		d := m.devices[m.cursor]
		b.WriteString(m.styles.title.Render(fmt.Sprintf("📋 %s", d.EntityID)))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  State: %s\n", d.State))
		if d.Attributes != nil {
			for _, key := range []string{"friendly_name", "brightness", "color_temp", "temperature", "humidity", "battery", "unit_of_measurement", "device_class"} {
				if val, ok := d.Attributes[key]; ok {
					b.WriteString(fmt.Sprintf("  %s: %v\n", key, val))
				}
			}
		}
	}

	// Help bar
	b.WriteString(m.styles.help.Render("↑/k ↓/j navigate │ r refresh │ q quit"))

	return b.String()
}

func newMatterDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Interactive TUI dashboard for Matter/IoT devices",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			client := newGatewayClient(cfg.SocketPath)

			p := tea.NewProgram(initialMatterModel(client), tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
}

func newMatterCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "matter", Short: "Manage Matter/IoT devices"}
	cmd.AddCommand(newMatterDashboardCmd(), newMatterListCmd(), newMatterStateCmd(), newMatterSetupCmd(), newMatterCommissionCmd())
	return cmd
}

func newMatterListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all Matter devices (non-interactive)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			client := newGatewayClient(cfg.SocketPath)
			var out struct {
				Connected bool                   `json:"connected"`
				Devices   map[string]interface{} `json:"devices"`
			}
			if err := client.request(cmd.Context(), "GET", "/matter", nil, &out); err != nil {
				return err
			}
			if !out.Connected {
				fmt.Println("Matter: not connected (Home Assistant not configured)")
				return nil
			}
			devices := parseDevices(out.Devices)
			if len(devices) == 0 {
				fmt.Println("No Matter devices found.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "DEVICE\tSTATE\tFRIENDLY NAME")
			for _, d := range devices {
				fn := ""
				if d.Attributes != nil {
					if v, ok := d.Attributes["friendly_name"].(string); ok {
						fn = v
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", d.EntityID, d.State, fn)
			}
			return tw.Flush()
		},
	}
}

func newMatterStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state <entity_id>",
		Short: "Show detailed state of a Matter device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			client := newGatewayClient(cfg.SocketPath)
			var out interface{}
			if err := client.request(cmd.Context(), "GET", "/matter/"+args[0], nil, &out); err != nil {
				return err
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
}