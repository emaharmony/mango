package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/carlosmaranje/mango/internal/agent"
	"github.com/carlosmaranje/mango/internal/skill"
)

var knownProviders = []string{"anthropic", "ollama", "openai-compatible"}

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Interactively create agents or skills",
	}
	cmd.AddCommand(newAddAgentCmd(), newAddSkillCmd())
	return cmd
}

func newAddAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent <name>",
		Short: "Interactively create a new agent (definition + config entry)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("agent name cannot be empty")
			}
			return runAddAgent(name, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout())
		},
	}
}

func newAddSkillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "skill <name>",
		Short: "Interactively create a new skill definition file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("skill name cannot be empty")
			}
			return runAddSkill(name, bufio.NewReader(cmd.InOrStdin()), cmd.OutOrStdout())
		},
	}
}

// runAddAgent handles the interactive agent scaffolding flow.
func runAddAgent(name string, in *bufio.Reader, out io.Writer) error {
	agentsDir := agent.ResolveAgentsDir("")
	path := agent.AgentDefinitionPath(agentsDir, name)

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("agent %q already exists at %s", name, path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	role, err := requireLine(in, out, "Role (e.g. You are a research assistant...): ", "role cannot be blank")
	if err != nil {
		return err
	}

	provider, err := requireChoice(in, out,
		fmt.Sprintf("LLM provider [%s]: ", strings.Join(knownProviders, "/")),
		knownProviders,
	)
	if err != nil {
		return err
	}

	model, err := requireLine(in, out, "Model (e.g. llama3.2): ", "model cannot be blank")
	if err != nil {
		return err
	}

	apiKey, err := readLine(in, out, "API key (optional, e.g. ${ANTHROPIC_API_KEY}): ")
	if err != nil {
		return err
	}

	baseURLDefault := providerBaseURLHint(provider)
	baseURLPrompt := "Base URL (optional"
	if baseURLDefault != "" {
		baseURLPrompt += ", default: " + baseURLDefault
	}
	baseURLPrompt += "): "
	baseURL, err := readLine(in, out, baseURLPrompt)
	if err != nil {
		return err
	}

	skillsRaw, err := readLine(in, out, "Skills (optional, comma-separated, e.g. web_search,code_execution): ")
	if err != nil {
		return err
	}
	skills := parseSkills(skillsRaw)

	definition := fmt.Sprintf("# %s\n\n%s\n", strings.ToUpper(name), role)
	if err := os.WriteFile(path, []byte(definition), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	configPathUsed, err := appendAgentToConfig(name, skills, provider, model, apiKey, baseURL)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "\nAgent %q created.\n", name)
	fmt.Fprintf(out, "  Definition: %s\n", path)
	fmt.Fprintf(out, "  Config updated: %s\n", configPathUsed)
	return nil
}

// runAddSkill handles the interactive skill scaffolding flow.
func runAddSkill(name string, in *bufio.Reader, out io.Writer) error {
	skillsDir := skill.ResolveSkillsDir("")
	path := filepath.Join(skillsDir, name+".md")

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("skill %q already exists at %s", name, path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", skillsDir, err)
	}

	fmt.Fprintln(out, "Enter skill description (markdown supported).")
	fmt.Fprintln(out, "Type your content, then press Enter twice to finish.")
	fmt.Fprintln(out, "Leave blank and press Enter to skip and fill in later.")
	fmt.Fprint(out, "> ")

	content, skipped, err := readMultilineContent(in)
	if err != nil {
		return err
	}

	if skipped {
		placeholder := fmt.Sprintf("# %s\n\nDescribe this skill here.\n", name)
		if err := os.WriteFile(path, []byte(placeholder), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Fprintf(out, "\nSkill %q created at %s with placeholder content. Edit the file to add the skill description.\n", name, path)
		return nil
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(out, "\nSkill %q created at %s.\n", name, path)
	return nil
}

// readLine prompts with label and returns the trimmed line (may be empty).
func readLine(in *bufio.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// requireLine repeatedly prompts until a non-blank line is provided.
func requireLine(in *bufio.Reader, out io.Writer, label, errHint string) (string, error) {
	for {
		s, err := readLine(in, out, label)
		if err != nil {
			return "", err
		}
		if s != "" {
			return s, nil
		}
		fmt.Fprintln(out, errHint)
	}
}

// requireChoice repeatedly prompts until the input matches one of choices.
func requireChoice(in *bufio.Reader, out io.Writer, label string, choices []string) (string, error) {
	for {
		s, err := readLine(in, out, label)
		if err != nil {
			return "", err
		}
		for _, c := range choices {
			if s == c {
				return s, nil
			}
		}
		fmt.Fprintf(out, "please enter one of: %s\n", strings.Join(choices, ", "))
	}
}

// readMultilineContent accumulates lines until two consecutive blank lines
// are entered. If the very first line is blank, the user is treated as having
// skipped (skipped=true, content="").
func readMultilineContent(in *bufio.Reader) (string, bool, error) {
	var lines []string
	consecutiveBlank := 0
	first := true

	for {
		line, err := in.ReadString('\n')
		eof := err == io.EOF
		if err != nil && !eof {
			return "", false, err
		}
		stripped := strings.TrimRight(line, "\r\n")

		if first {
			first = false
			if stripped == "" {
				return "", true, nil
			}
		}

		if stripped == "" {
			consecutiveBlank++
			if consecutiveBlank >= 2 {
				break
			}
		} else {
			consecutiveBlank = 0
		}

		lines = append(lines, stripped)
		if eof {
			break
		}
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n"), false, nil
}

func providerBaseURLHint(provider string) string {
	switch provider {
	case "anthropic":
		return "https://api.anthropic.com"
	case "ollama":
		return "http://localhost:11434/v1"
	case "openai-compatible":
		return ""
	default:
		return ""
	}
}

// appendAgentToConfig writes the new agent entry under the `agents:` key in
// the viper-resolved config file, preserving existing content. Returns the
// path of the file that was written.
func appendAgentToConfig(name string, skills []string, provider, model, apiKey, baseURL string) (string, error) {
	v, err := loadRawViper(configPath)
	if err != nil {
		return "", err
	}

	var agents []AgentConfig
	if err := v.UnmarshalKey("agents", &agents); err != nil {
		return "", err
	}
	for _, a := range agents {
		if a.Name == name {
			return "", fmt.Errorf("agent %q already exists in config", name)
		}
	}

	entry := AgentConfig{
		Name:    name,
		WorkDir: fmt.Sprintf("/var/lib/mango/agents/%s", name),
		Skills:  skills,
		LLM: LLMConfig{
			Provider: provider,
			Model:    model,
			APIKey:   apiKey,
			BaseURL:  baseURL,
		},
	}
	agents = append(agents, entry)
	v.Set("agents", agents)

	path := v.ConfigFileUsed()
	if path == "" {
		path = configPath
		if path == "" {
			path = defaultConfigPath()
		}
	}
	if err := writeViperConfig(v); err != nil {
		return "", err
	}
	return path, nil
}
