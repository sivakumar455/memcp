// Package config handles configuration loading and management for memcp.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the top-level configuration for memcp.
type Config struct {
	Persona     PersonaConfig     `mapstructure:"persona"`
	Memory      MemoryConfig      `mapstructure:"memory"`
	Logging     LoggingConfig     `mapstructure:"logging"`
	Evolution   EvolutionConfig   `mapstructure:"evolution"`
	Observation ObservationConfig `mapstructure:"observation"`
	Profile     ProfileConfig     `mapstructure:"profile"`
	Context     ContextConfig     `mapstructure:"context"`
	Skills      SkillsConfig      `mapstructure:"skills"`
	Daemon      DaemonConfig      `mapstructure:"daemon"`
	Gateway     GatewayConfig     `mapstructure:"gateway"`
}

// PersonaConfig controls persona/soul file loading.
type PersonaConfig struct {
	SoulDir         string `mapstructure:"soul_dir"`
	MaxCharsPerFile int    `mapstructure:"max_chars_per_file"`
	TotalMaxChars   int    `mapstructure:"total_max_chars"`
}

// MemoryConfig controls the SQLite memory store.
type MemoryConfig struct {
	DBPath          string             `mapstructure:"db_path"`
	ContextWindow   int                `mapstructure:"context_window"`
	MaxContextChars int                `mapstructure:"max_context_chars"`
	VectorSearch    VectorSearchConfig `mapstructure:"vector_search"`
}

// VectorSearchConfig controls semantic vector search options.
type VectorSearchConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Provider  string `mapstructure:"provider"`
	OllamaURL string `mapstructure:"ollama_url"`
	ModelName string `mapstructure:"model_name"`
}

// LoggingConfig controls log output.
type LoggingConfig struct {
	Dir      string `mapstructure:"dir"`
	Level    string `mapstructure:"level"`
	MaxFiles int    `mapstructure:"max_files"`
}

// EvolutionConfig controls persona/memory evolution.
type EvolutionConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	Trigger             string `mapstructure:"trigger"`
	MinFindings         int    `mapstructure:"min_findings"`
	MinMessages         int    `mapstructure:"min_messages"`
	MaxMemoryEntries    int    `mapstructure:"max_memory_entries"`
	MaxIdentityPatterns int    `mapstructure:"max_identity_patterns"`
	CompactionInterval  int    `mapstructure:"compaction_interval"`
}

// ObservationConfig controls tool call observation.
type ObservationConfig struct {
	Enabled          bool `mapstructure:"enabled"`
	MaxResultSummary int  `mapstructure:"max_result_summary"`
	MaxFactsPerCall  int  `mapstructure:"max_facts_per_call"`
	AsyncUpsert      bool `mapstructure:"async_upsert"`
}

// ProfileConfig controls user behavior profiling.
type ProfileConfig struct {
	Enabled       bool `mapstructure:"enabled"`
	MaxEntries    int  `mapstructure:"max_entries"`
	TopNInContext int  `mapstructure:"top_n_in_context"`
}

// ContextConfig controls the tiered context budget allocation.
type ContextConfig struct {
	MaxChars       int `mapstructure:"max_chars"`
	CoreBudgetPct  int `mapstructure:"core_budget_pct"`
	WorkBudgetPct  int `mapstructure:"work_budget_pct"`
	RelevBudgetPct int `mapstructure:"relev_budget_pct"`
	HistBudgetPct  int `mapstructure:"hist_budget_pct"`
}

// SkillsConfig controls skill loading and routing.
type SkillsConfig struct {
	Dir                 string        `mapstructure:"dir"`
	AutoEvolve          bool          `mapstructure:"auto_evolve"`
	MaxPatternsPerSkill int           `mapstructure:"max_patterns_per_skill"`
	MaxCharsPerSkill    int           `mapstructure:"max_chars_per_skill"`
	DomainRouting       DomainRouting `mapstructure:"domain_routing"`
}

// DomainRouting maps tool prefixes and tags to skill domains.
type DomainRouting struct {
	ToolPrefixes map[string]string `mapstructure:"tool_prefixes"`
	TagMapping   map[string]string `mapstructure:"tag_mapping"`
}

// DaemonConfig holds options for background polling and watchers.
type DaemonConfig struct {
	Enabled     bool              `mapstructure:"enabled"`
	Interval    int               `mapstructure:"interval"`
	Watchers    WatchersConfig    `mapstructure:"watchers"`
	Rules       []ClassifierRule  `mapstructure:"rules"`
	Gateway     DaemonGateway     `mapstructure:"gateway"`
	Cleanup     CleanupConfig     `mapstructure:"cleanup"`
	CursorAgent CursorAgentConfig `mapstructure:"cursor_agent"`
}

// WatchersConfig groups all daemon watcher configurations.
type WatchersConfig struct {
	Jira      JiraWatcherConfig      `mapstructure:"jira"`
	Email     EmailWatcherConfig     `mapstructure:"email"`
	Teams     TeamsWatcherConfig     `mapstructure:"teams"`
	Bitbucket BitbucketWatcherConfig `mapstructure:"bitbucket"`
}

// JiraWatcherConfig configures the Jira polling watcher.
type JiraWatcherConfig struct {
	Enabled       bool              `mapstructure:"enabled"`
	Interval      int               `mapstructure:"interval"`
	Command       string            `mapstructure:"command"`
	Args          []string          `mapstructure:"args"`
	Env           map[string]string `mapstructure:"env"`
	JQL           string            `mapstructure:"jql"`
	ProjectFilter []string          `mapstructure:"project_filter"`
}

// EmailWatcherConfig configures the email polling watcher.
type EmailWatcherConfig struct {
	Enabled    bool              `mapstructure:"enabled"`
	Interval   int               `mapstructure:"interval"`
	Command    string            `mapstructure:"command"`
	Args       []string          `mapstructure:"args"`
	Env        map[string]string `mapstructure:"env"`
	DigestPath string            `mapstructure:"digest_path"`
}

// TeamsWatcherConfig configures the MS Teams polling watcher.
type TeamsWatcherConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	Interval      int      `mapstructure:"interval"`
	GraphEndpoint string   `mapstructure:"graph_endpoint"`
	PollChats     bool     `mapstructure:"poll_chats"`
	PollChannels  []string `mapstructure:"poll_channels"`
}

// BitbucketWatcherConfig configures the Bitbucket PR polling watcher.
type BitbucketWatcherConfig struct {
	Enabled       bool                   `mapstructure:"enabled"`
	Interval      int                    `mapstructure:"interval"`
	Command       string                 `mapstructure:"command"`
	Args          []string               `mapstructure:"args"`
	Env           map[string]string      `mapstructure:"env"`
	BaseURL       string                 `mapstructure:"base_url"`
	Token         string                 `mapstructure:"token"`
	ProjectFilter []string               `mapstructure:"project_filter"`
	RepoFilter    []string               `mapstructure:"repo_filter"`
	IncludeDiff   bool                   `mapstructure:"include_diff"`
	MaxDiffLines  int                    `mapstructure:"max_diff_lines"`
	Webhook       BitbucketWebhookConfig `mapstructure:"webhook"`
}

// BitbucketWebhookConfig controls inbound Bitbucket webhook processing.
type BitbucketWebhookConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Secret  string `mapstructure:"secret"`
}

// ClassifierRule defines a classification rule for daemon events.
type ClassifierRule struct {
	Match    RuleMatch `mapstructure:"match"`
	Priority string    `mapstructure:"priority"`
	Action   string    `mapstructure:"action"`
}

// RuleMatch defines conditions for matching daemon events.
type RuleMatch struct {
	Source  string `mapstructure:"source"`
	Field   string `mapstructure:"field"`
	Value   string `mapstructure:"value"`
	Pattern string `mapstructure:"pattern"`
}

// DaemonGateway configures the daemon HTTP API server.
type DaemonGateway struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// CleanupConfig controls automatic task cleanup.
type CleanupConfig struct {
	MaxAgeDays int  `mapstructure:"max_age_days"`
	RunOnStart bool `mapstructure:"run_on_start"`
}

// CursorAgentConfig controls the Cursor CLI agent integration.
type CursorAgentConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	CursorPath string `mapstructure:"cursor_path"`
	Timeout    int    `mapstructure:"timeout"`
	WorkDir    string `mapstructure:"work_dir"`
}

// GatewayConfig holds options for the HTTP API server.
type GatewayConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Address string `mapstructure:"address"`
}

// BaseDir is the resolved base directory of the memcp binary.
var BaseDir string

// Cfg is the global config singleton, populated by Load().
var Cfg Config

// Load reads the configuration from YAML files.
// Search priority:
//  1. XDG config dir (~/.config/memcp/config.yaml)
//  2. {dataDir}/configs/  (e.g. ~/.memcp/configs/)
//  3. ./site/configs/     (company overrides, excluded from public repo)
//  4. ./configs/          (upstream defaults)
//
// Finally, it merges any project-specific overrides from ./.memcp.yaml.
func Load(dataDir string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Environment variable overrides
	v.SetEnvPrefix("MEMCP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	globalLoaded := false

	// 1. Attempt to load global config from XDG Base Directory
	if configDir, err := os.UserConfigDir(); err == nil {
		v.SetConfigFile(filepath.Join(configDir, "memcp", "config.yaml"))
		if err := v.ReadInConfig(); err == nil {
			globalLoaded = true
		}
	}

	// 2. Fallback: search dataDir, site/configs, configs, cwd
	if !globalLoaded {
		configName := os.Getenv("MEMCP_CONFIG")
		if configName == "" {
			configName = "standalone"
		}
		v.SetConfigName(configName)
		v.SetConfigType("yaml")
		if dataDir != "" {
			v.AddConfigPath(filepath.Join(dataDir, "configs"))
		}
		v.AddConfigPath("./site/configs")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")

		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("reading legacy config: %w", err)
			}
		}
	}

	// 3. Merge local override (.memcp.yaml) from current working directory
	localViper := viper.New()
	localViper.SetConfigFile(".memcp.yaml")
	if err := localViper.ReadInConfig(); err == nil {
		if err := v.MergeConfigMap(localViper.AllSettings()); err != nil {
			fmt.Fprintf(os.Stderr, "memcp: warning: failed to merge .memcp.yaml: %v\n", err)
		}
	}

	// Override log level from env
	if lvl := os.Getenv("MEMCP_LOG_LEVEL"); lvl != "" {
		v.Set("logging.level", lvl)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Resolve relative paths against dataDir if provided
	if dataDir != "" {
		cfg.resolvePaths(dataDir)
	}

	return &cfg, nil
}

func (c *Config) resolvePaths(base string) {
	if !filepath.IsAbs(c.Persona.SoulDir) {
		c.Persona.SoulDir = filepath.Join(base, c.Persona.SoulDir)
	}
	if !filepath.IsAbs(c.Memory.DBPath) {
		c.Memory.DBPath = filepath.Join(base, c.Memory.DBPath)
	}
	if !filepath.IsAbs(c.Logging.Dir) {
		c.Logging.Dir = filepath.Join(base, c.Logging.Dir)
	}
	if !filepath.IsAbs(c.Skills.Dir) {
		c.Skills.Dir = filepath.Join(base, c.Skills.Dir)
	}
}

func setDefaults(v *viper.Viper) {
	// Persona
	v.SetDefault("persona.soul_dir", "./soul")
	v.SetDefault("persona.max_chars_per_file", 20000)
	v.SetDefault("persona.total_max_chars", 100000)

	// Memory
	v.SetDefault("memory.db_path", "./data/memory.db")
	v.SetDefault("memory.context_window", 50)
	v.SetDefault("memory.max_context_chars", 80000)
	v.SetDefault("memory.vector_search.enabled", false)
	v.SetDefault("memory.vector_search.provider", "ollama")
	v.SetDefault("memory.vector_search.ollama_url", "http://localhost:11434")
	v.SetDefault("memory.vector_search.model_name", "nomic-embed-text")

	// Logging
	v.SetDefault("logging.dir", "./tmp")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.max_files", 5)

	// Evolution
	v.SetDefault("evolution.enabled", true)
	v.SetDefault("evolution.trigger", "on_save")
	v.SetDefault("evolution.min_findings", 1)
	v.SetDefault("evolution.min_messages", 20)
	v.SetDefault("evolution.max_memory_entries", 100)
	v.SetDefault("evolution.max_identity_patterns", 30)
	v.SetDefault("evolution.compaction_interval", 50)

	// Observation
	v.SetDefault("observation.enabled", true)
	v.SetDefault("observation.max_result_summary", 500)
	v.SetDefault("observation.max_facts_per_call", 5)
	v.SetDefault("observation.async_upsert", true)

	// Profile
	v.SetDefault("profile.enabled", true)
	v.SetDefault("profile.max_entries", 200)
	v.SetDefault("profile.top_n_in_context", 10)

	// Context tiers
	v.SetDefault("context.max_chars", 80000)
	v.SetDefault("context.core_budget_pct", 20)
	v.SetDefault("context.work_budget_pct", 30)
	v.SetDefault("context.relev_budget_pct", 30)
	v.SetDefault("context.hist_budget_pct", 20)

	// Skills
	v.SetDefault("skills.dir", "./skills")
	v.SetDefault("skills.auto_evolve", true)
	v.SetDefault("skills.max_patterns_per_skill", 20)
	v.SetDefault("skills.max_chars_per_skill", 15000)

	// Daemon
	v.SetDefault("daemon.enabled", false)
	v.SetDefault("daemon.interval", 300)
	v.SetDefault("daemon.cleanup.max_age_days", 30)
	v.SetDefault("daemon.cleanup.run_on_start", true)

	// Gateway
	v.SetDefault("gateway.enabled", false)
	v.SetDefault("gateway.address", "127.0.0.1:12345")
}
