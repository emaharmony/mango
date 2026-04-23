package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newMatterSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Install Matter controller dependencies (Node.js + matter.js)",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, _ := os.UserHomeDir()
			matterDir := filepath.Join(home, ".mango", "matter")

			// Check node is available
			nodePath, err := exec.LookPath("node")
			if err != nil {
				return fmt.Errorf("Node.js not found — install it first: https://nodejs.org")
			}
			fmt.Printf("✅ Node.js found: %s\n", nodePath)

			// Create matter directory
			if err := os.MkdirAll(matterDir, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", matterDir, err)
			}
			fmt.Printf("✅ Created directory: %s\n", matterDir)

			// Copy controller script
			scriptSrc := filepath.Join("config", "matter", "matter-controller.mjs")
			scriptDst := filepath.Join(matterDir, "matter-controller.mjs")

			// Try to find the script in various locations
			searchPaths := []string{
				scriptSrc,
				filepath.Join("internal", "matter", "scripts", "matter-controller.mjs"),
			}
			found := ""
			for _, p := range searchPaths {
				if _, err := os.Stat(p); err == nil {
					found = p
					break
				}
			}

			if found != "" {
				data, err := os.ReadFile(found)
				if err != nil {
					return fmt.Errorf("read controller script: %w", err)
				}
				if err := os.WriteFile(scriptDst, data, 0o644); err != nil {
					return fmt.Errorf("write controller script: %w", err)
				}
				fmt.Printf("✅ Controller script installed: %s\n", scriptDst)
			} else {
				// Generate the script inline
				fmt.Println("⚠️  Controller script not found in source tree — generating default")
				generateDefaultScript(scriptDst)
				fmt.Printf("✅ Default controller script generated: %s\n", scriptDst)
			}

			// Install npm dependencies
			fmt.Println("📦 Installing @project-chip/matter-node.js...")
			npmCmd := exec.Command("npm", "init", "-y")
			npmCmd.Dir = matterDir
			npmCmd.Run()

			installCmd := exec.Command("npm", "install", "@project-chip/matter-node.js")
			installCmd.Dir = matterDir
			installCmd.Stdout = os.Stdout
			installCmd.Stderr = os.Stderr
			if err := installCmd.Run(); err != nil {
				return fmt.Errorf("npm install failed: %w", err)
			}
			fmt.Println("✅ matter-node.js installed")

			// Create storage directory
			storageDir := filepath.Join(matterDir, "storage")
			os.MkdirAll(storageDir, 0o755)

			fmt.Println()
			fmt.Println("🏠 Matter controller setup complete!")
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  1. Add to config.yaml:")
			fmt.Println("       matter:")
			fmt.Println("         enabled: true")
			fmt.Println("  2. Start Mango: mango serve")
			fmt.Println("  3. Commission a device: mango matter commission <QR_CODE>")
			fmt.Println("  4. View devices: mango matter dashboard")
			fmt.Println()
			fmt.Printf("Controller script: %s\n", scriptDst)
			fmt.Printf("Storage: %s\n", storageDir)

			return nil
		},
	}
}

func newMatterCommissionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "commission <qr_or_pairing_code>",
		Short: "Commission a new Matter device using a QR code or manual pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			client := newGatewayClient(cfg.SocketPath)

			// Send commission request through the gateway
			var out map[string]interface{}
			if err := client.request(cmd.Context(), "POST", "/matter/commission", map[string]string{"code": args[0]}, &out); err != nil {
				return fmt.Errorf("commission request failed: %w", err)
			}

			fmt.Printf("Commissioning device with code: %s\n", args[0])
			if success, ok := out["success"].(bool); ok && success {
				fmt.Println("✅ Device commissioned successfully!")
			} else {
				fmt.Printf("❌ Commissioning failed: %v\n", out["error"])
			}
			return nil
		},
	}
}

func newMatterCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "matter", Short: "Manage Matter/IoT devices"}
	cmd.AddCommand(newMatterSetupCmd(), newMatterCommissionCmd())
	return cmd
}

func generateDefaultScript(path string) {
	// Minimal fallback script that just logs and communicates with Mango
	script := `#!/usr/bin/env node
// Mango Matter Controller — Auto-generated stub
// Run ` + "`mango matter setup`" + ` with the full source tree to get the real controller.

function sendMessage(type, data) {
  process.stdout.write(JSON.stringify({ type, data }) + "\n");
}

sendMessage("log", { message: "Matter controller stub running — install full controller for device support" });
sendMessage("ready", { device_count: 0, stub: true });

process.stdin.on("data", (chunk) => {
  for (const line of chunk.toString().split("\n")) {
    if (!line.trim()) continue;
    try {
      const msg = JSON.parse(line);
      if (msg.type === "shutdown") {
        process.exit(0);
      }
      sendMessage("error", { message: "Stub mode: install full controller with 'mango matter setup'" });
    } catch (_) {}
  }
});
`
	os.WriteFile(path, []byte(script), 0o644)
}