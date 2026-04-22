package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/carlosmaranje/mango/internal/agent"
	"github.com/carlosmaranje/mango/internal/constants"
	"github.com/carlosmaranje/mango/internal/discord"
	"github.com/carlosmaranje/mango/internal/gateway"
	"github.com/carlosmaranje/mango/internal/llm"
	"github.com/carlosmaranje/mango/internal/memory"
	"github.com/carlosmaranje/mango/internal/orchestrator"
	"github.com/carlosmaranje/mango/internal/matter"
	"github.com/carlosmaranje/mango/internal/skill"
	"github.com/carlosmaranje/mango/internal/tools"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the gateway in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			return runServe(cmd.Context(), cfg)
		},
	}
}

func runServe(parent context.Context, cfg *Config) error {
	ctx, cancel := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	agentsDir := agent.ResolveAgentsDir("")
	skillsDir := skill.ResolveSkillsDir("")
	skillLoader := skill.NewLoader(skillsDir)

	registry := agent.NewRegistry()
	runners := map[string]*agent.Runner{}
	closers := []func() error{}
	toolReg := tools.NewRegistry()

	if err := toolReg.Register(tools.NewGoSolarTool()); err != nil {
		return fmt.Errorf("failed to register gosolar tool: %w", err)
	}
	// Memory search and recall tools are available to all agents
	// (they'll be registered per-agent below with the agent's memory store)

	var orchestratorAgent *agent.Agent

	for _, ac := range cfg.Agents {
		if ac.LLM.Provider == "" {
			log.Printf("warn: agent %q has no LLM provider configured — skipping. Edit config and restart.", ac.Name)
			continue
		}

		llmClient, err := llm.NewClient(llm.ProviderConfig{
			Provider: ac.LLM.Provider,
			Model:    ac.LLM.Model,
			APIKey:   ac.LLM.APIKey,
			BaseURL:  ac.LLM.BaseURL,
		})
		if err != nil {
			return fmt.Errorf("agent %q: %w", ac.Name, err)
		}

		workDir := ac.WorkDir
		if workDir == "" {
			workDir = filepath.Join(os.TempDir(), constants.AppName, ac.Name)
		}
		mem, err := memory.Open(workDir)
		if err != nil {
			return fmt.Errorf("agent %q memory: %w", ac.Name, err)
		}
		closers = append(closers, mem.Close)

		systemPrompt, err := agent.ComposeSystemPrompt(agentsDir, ac.Name, ac.Skills, skillLoader)
		if err != nil {
			return fmt.Errorf("agent %q: %w", ac.Name, err)
		}
		if len(ac.Skills) > 0 {
			log.Printf("agent %q: loaded skills %v from %s", ac.Name, ac.Skills, skillsDir)
		}

		a := &agent.Agent{
			Name:         ac.Name,
			WorkDir:      workDir,
			LLM:          llmClient,
			Skills:       ac.Skills,
			SystemPrompt: systemPrompt,
			Memory:       mem,
			Session:      agent.NewSessionStore(),
			AuthCreds:    ac.AuthCreds,
		}

		// Register memory tools for this agent
		// All agents get search and recall (read-only)
		memSearch := tools.NewMemorySearchTool(mem)
		memRecall := tools.NewMemoryRecallTool(mem)
		if err := toolReg.Register(memSearch); err != nil {
			return fmt.Errorf("agent %q: register memory_search: %w", ac.Name, err)
		}
		if err := toolReg.Register(memRecall); err != nil {
			return fmt.Errorf("agent %q: register memory_recall: %w", ac.Name, err)
		}

		// Manager agents also get store and config (write access)
		if ac.Role == "orchestrator" || ac.Role == "manager" {
			memStore := tools.NewMemoryStoreTool(mem)
			memConfig := tools.NewMemoryConfigTool(mem)
			if err := toolReg.Register(memStore); err != nil {
				return fmt.Errorf("agent %q: register memory_store: %w", ac.Name, err)
			}
			if err := toolReg.Register(memConfig); err != nil {
				return fmt.Errorf("agent %q: register memory_config: %w", ac.Name, err)
			}
		}
		if err := registry.Register(a); err != nil {
			return err
		}
		runner := agent.NewRunner(a, toolReg, 0)
		runners[a.Name] = runner
		if err := runner.Start(ctx); err != nil {
			return err
		}
		if ac.Role == "orchestrator" {
			orchestratorAgent = a
		}
	}

	if len(runners) == 0 {
		log.Printf("warn: no agents configured — tasks will fail. Run 'mango agent create' or edit configuration.")
	}

	var orch *orchestrator.Orchestrator
	if orchestratorAgent != nil {
		orch = orchestrator.NewOrchestrator(orchestratorAgent, registry)
	}
	dispatcher := orchestrator.NewDispatcher(registry, runners, orch)

	gw := gateway.NewServer(cfg.SocketPath, registry, runners, dispatcher)
	if err := gw.Start(ctx); err != nil {
		return err
	}
	log.Printf("gateway: listening on %s", cfg.SocketPath)

	if cfg.Discord.Token != "" {
		bindings := make([]discord.ChannelBinding, 0, len(cfg.Bindings))
		for _, b := range cfg.Bindings {
			bindings = append(bindings, discord.ChannelBinding{ChannelID: b.ChannelID, AgentName: b.Agent})
		}
		router := discord.NewRouter(bindings)
		history := discord.NewChannelHistory(discord.DefaultHistorySize)
		bot, err := discord.NewBot(cfg.Discord.Token, router, history, dispatcher, cfg.Discord.Global)
		if err != nil {
			return err
		}
		defer func() {
			if err := bot.Close(); err != nil {
				log.Printf("discord: close: %v", err)
			}
		}()

		if err := bot.Start(ctx); err != nil {
			return err
		}
	} else {
		log.Printf("discord: no token configured, skipping")
	}

	// Start Matter (IoT) channel if configured
	if cfg.Matter.Enabled {
		matterBot, err := matter.NewBot(matter.BotConfig{
			Controller: matter.ControllerConfig{
				Enabled:          cfg.Matter.Enabled,
				NodePath:         cfg.Matter.NodePath,
				StorageDir:       cfg.Matter.StorageDir,
				NetworkInterface: cfg.Matter.NetworkInterface,
				Port:             cfg.Matter.Port,
			},
			EntityFilters:  cfg.Matter.EntityFilters,
			AgentBindings:  cfg.Matter.AgentBindings,
		}, dispatcher)
		if err != nil {
			return fmt.Errorf("matter: %w", err)
		}
		if err := matterBot.Start(ctx); err != nil {
			return fmt.Errorf("matter start: %w", err)
		}

	// Register Matter as an agent tool
		matterTool := matter.NewTool(matterBot)
		if err := toolReg.Register(matterTool); err != nil {
			return fmt.Errorf("register matter tool: %w", err)
		}

		// Wire Matter into gateway for /matter endpoints
		gw.SetMatterBot(matterBot)

		defer func() {
			if err := matterBot.Close(); err != nil {
				log.Printf("matter: close: %v", err)
			}
		}()
	} else {
		log.Printf("matter: no Home Assistant configured, skipping")
	}

	<-ctx.Done()
	log.Printf("shutdown: stopping runners")
	for _, r := range runners {
		r.Stop()
	}
	for _, c := range closers {
		_ = c()
	}
	return nil
}
