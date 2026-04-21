package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"

	"github.com/carlosmaranje/mango/internal/constants"
)

type LLMConfig struct {
	Provider string `mapstructure:"provider" yaml:"provider"`
	Model    string `mapstructure:"model" yaml:"model"`
	APIKey   string `mapstructure:"api_key" yaml:"api_key,omitempty"`
	BaseURL  string `mapstructure:"base_url" yaml:"base_url,omitempty"`
}

type AgentConfig struct {
	Name      string            `mapstructure:"name" yaml:"name"`
	WorkDir   string            `mapstructure:"work_dir" yaml:"work_dir,omitempty"`
	Role      string            `mapstructure:"role" yaml:"role,omitempty"`
	Skills    []string          `mapstructure:"skills" yaml:"skills,omitempty"`
	LLM       LLMConfig         `mapstructure:"llm" yaml:"llm"`
	AuthCreds map[string]string `mapstructure:"auth_creds" yaml:"auth_creds,omitempty"`
}

type BindingConfig struct {
	ChannelID string `mapstructure:"channel_id" yaml:"channel_id"`
	Agent     string `mapstructure:"agent" yaml:"agent"`
}

type DiscordConfig struct {
	Token  string `mapstructure:"token" yaml:"token,omitempty"`
	Global bool   `mapstructure:"global" yaml:"global,omitempty"`
}

type LLMDefaultsConfig struct {
	Provider string `mapstructure:"provider" yaml:"provider,omitempty"`
	Model    string `mapstructure:"model" yaml:"model,omitempty"`
	APIKey   string `mapstructure:"api_key" yaml:"api_key,omitempty"`
	BaseURL  string `mapstructure:"base_url" yaml:"base_url,omitempty"`
}

type MatterConfig struct {
	Enabled        bool              `mapstructure:"enabled" yaml:"enabled,omitempty"`
	NodePath       string            `mapstructure:"node_path" yaml:"node_path,omitempty"`
	StorageDir     string            `mapstructure:"storage_dir" yaml:"storage_dir,omitempty"`
	NetworkInterface string          `mapstructure:"network_interface" yaml:"network_interface,omitempty"`
	Port           int               `mapstructure:"port" yaml:"port,omitempty"`
	EntityFilters  []string          `mapstructure:"entity_filters" yaml:"entity_filters,omitempty"`
	AgentBindings  map[string]string `mapstructure:"agent_bindings" yaml:"agent_bindings,omitempty"`
}

type Config struct {
	SocketPath  string             `mapstructure:"socket_path" yaml:"socket_path,omitempty"`
	Discord     DiscordConfig      `mapstructure:"discord" yaml:"discord,omitempty"`
	Matter      MatterConfig       `mapstructure:"matter" yaml:"matter,omitempty"`
	LLMDefaults LLMDefaultsConfig  `mapstructure:"llm_defaults" yaml:"llm_defaults,omitempty"`
	Agents     []AgentConfig      `mapstructure:"agents" yaml:"agents,omitempty"`
	Bindings   []BindingConfig    `mapstructure:"bindings" yaml:"bindings,omitempty"`

	ConfigDir string `mapstructure:"-" yaml:"-"`
}

func defaultSocketPath() string {
	if envPath := os.Getenv("MANGO_SOCKET_PATH"); envPath != "" {
		return envPath
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, "."+constants.AppName, constants.AppName+".sock")
		}
	}
	return fmt.Sprintf("/var/run/%s/%s.sock", constants.AppName, constants.AppName)
}

func defaultConfigPath() string {
	if envPath := os.Getenv("MANGO_CONFIG"); envPath != "" {
		return envPath
	}
	return fmt.Sprintf("/etc/%s/config.yaml", constants.AppName)
}

func loadRawViper(path string) (*viper.Viper, error) {
	v := viper.New()
	v.SetDefault("socket_path", defaultSocketPath())

	// Use MANGO_CONFIG if path is not provided via flag
	if path == "" {
		path = os.Getenv("MANGO_CONFIG")
	}

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			var configFileNotFoundError viper.ConfigFileNotFoundError
			if !errors.As(err, &configFileNotFoundError) && !os.IsNotExist(err) {
				return nil, fmt.Errorf("read config %s: %w", path, err)
			}
		}
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(fmt.Sprintf("/etc/%s", constants.AppName))
		v.AddConfigPath("./config")
		v.AddConfigPath(".")
		if err := v.ReadInConfig(); err != nil {
			// If no config file found, set the default path for future writes
			v.SetConfigFile(defaultConfigPath())
		}
	}
	return v, nil
}

func loadConfig(path string) (*Config, error) {
	v, err := loadRawViper(path)
	if err != nil {
		return nil, err
	}
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	expandConfig(&cfg)
	if cfg.SocketPath == "" {
		cfg.SocketPath = defaultSocketPath()
	}
	if used := v.ConfigFileUsed(); used != "" {
		cfg.ConfigDir = filepath.Dir(used)
	}
	return &cfg, nil
}

func expandConfig(cfg *Config) {
	cfg.SocketPath = os.ExpandEnv(cfg.SocketPath)
	cfg.Discord.Token = os.ExpandEnv(cfg.Discord.Token)

	// Expand LLM defaults
	cfg.LLMDefaults.Provider = os.ExpandEnv(cfg.LLMDefaults.Provider)
	cfg.LLMDefaults.Model = os.ExpandEnv(cfg.LLMDefaults.Model)
	cfg.LLMDefaults.APIKey = os.ExpandEnv(cfg.LLMDefaults.APIKey)
	cfg.LLMDefaults.BaseURL = os.ExpandEnv(cfg.LLMDefaults.BaseURL)

	// Expand Matter config
	cfg.Matter.NodePath = os.ExpandEnv(cfg.Matter.NodePath)
	cfg.Matter.StorageDir = os.ExpandEnv(cfg.Matter.StorageDir)
	cfg.Matter.NetworkInterface = os.ExpandEnv(cfg.Matter.NetworkInterface)

	// Apply defaults to agents that don't override
	for i := range cfg.Agents {
		a := &cfg.Agents[i]
		if a.LLM.Provider == "" {
			a.LLM.Provider = cfg.LLMDefaults.Provider
		}
		if a.LLM.Model == "" {
			a.LLM.Model = cfg.LLMDefaults.Model
		}
		if a.LLM.APIKey == "" {
			a.LLM.APIKey = cfg.LLMDefaults.APIKey
		}
		if a.LLM.BaseURL == "" {
			a.LLM.BaseURL = cfg.LLMDefaults.BaseURL
		}

		a.WorkDir = os.ExpandEnv(a.WorkDir)
		a.LLM.APIKey = os.ExpandEnv(a.LLM.APIKey)
		a.LLM.BaseURL = os.ExpandEnv(a.LLM.BaseURL)
		for k, v := range a.AuthCreds {
			a.AuthCreds[k] = os.ExpandEnv(v)
		}
	}
}
