package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultModel                = "anthropic/claude-sonnet-4-6"
	DefaultOpenClawGatewayToken = "e2b-agents-local-token"
)

type Config struct {
	AppAddr                  string
	DatabaseURL              string
	DatabaseMaxOpenConns     int
	DatabaseMaxIdleConns     int
	SlackClientID            string
	SlackClientSecret        string
	SlackSigningSecret       string
	SlackBotToken            string
	SlackAppToken            string
	SlackRedirectURL         string
	SlackDefaultTeamID       string
	SlackDefaultTemplateID   string
	E2BAPIKey                string
	AnthropicAPIKey          string
	RuntimeModel             string
	E2BHelperNode            string
	E2BHelperScript          string
	OpenClawGatewayPort      int
	ACPAdapterPort           int
	OpenClawGatewayToken     string
	SandboxTimeout           time.Duration
	SandboxRequestTimeout    time.Duration
	SlackProcessingTimeout   time.Duration
	WorkspaceAutoCreate      bool
	WorkspaceDefaultTeamID   string
	WorkspaceDefaultTemplate string
}

func Load() Config {
	defaultTemplate := getenv("SLACK_DEFAULT_TEMPLATE_ID")
	if defaultTemplate == "" {
		defaultTemplate = getenvDefault("E2B_AGENTS_DEFAULT_TEMPLATE_ID", "openclaw")
	}
	defaultTeam := getenv("SLACK_DEFAULT_TEAM_ID")
	if defaultTeam == "" {
		defaultTeam = getenvDefault("E2B_AGENTS_DEFAULT_TEAM_ID", "default")
	}

	return Config{
		AppAddr:                  getenvDefault("APP_ADDR", ":8080"),
		DatabaseURL:              getenv("DATABASE_URL"),
		DatabaseMaxOpenConns:     intDefault("DB_MAX_OPEN_CONNS", 10),
		DatabaseMaxIdleConns:     intDefault("DB_MAX_IDLE_CONNS", 5),
		SlackClientID:            getenv("SLACK_CLIENT_ID"),
		SlackClientSecret:        getenv("SLACK_CLIENT_SECRET"),
		SlackSigningSecret:       getenv("SLACK_SIGNING_SECRET"),
		SlackBotToken:            getenv("SLACK_BOT_TOKEN"),
		SlackAppToken:            getenv("SLACK_APP_TOKEN"),
		SlackRedirectURL:         getenv("SLACK_REDIRECT_URL"),
		SlackDefaultTeamID:       defaultTeam,
		SlackDefaultTemplateID:   defaultTemplate,
		E2BAPIKey:                getenv("E2B_API_KEY"),
		AnthropicAPIKey:          getenv("ANTHROPIC_API_KEY"),
		RuntimeModel:             getenvDefault("E2B_AGENTS_RUNTIME_MODEL", DefaultModel),
		E2BHelperNode:            getenvDefault("E2B_HELPER_NODE", "node"),
		E2BHelperScript:          getenvDefault("E2B_HELPER_SCRIPT", filepath.Join("runtime", "e2b-helper", "dist", "helper.js")),
		OpenClawGatewayPort:      intDefault("OPENCLAW_GATEWAY_PORT", 18789),
		ACPAdapterPort:           intDefault("E2B_AGENTS_ACP_ADAPTER_PORT", 18790),
		OpenClawGatewayToken:     getenvDefault("OPENCLAW_GATEWAY_TOKEN", DefaultOpenClawGatewayToken),
		SandboxTimeout:           durationDefault("E2B_SANDBOX_TIMEOUT", time.Hour),
		SandboxRequestTimeout:    durationDefault("E2B_SANDBOX_REQUEST_TIMEOUT", 5*time.Minute),
		SlackProcessingTimeout:   durationDefault("SLACK_PROCESSING_TIMEOUT", 10*time.Minute),
		WorkspaceAutoCreate:      boolDefault("E2B_AGENTS_WORKSPACE_AUTO_CREATE", true),
		WorkspaceDefaultTeamID:   defaultTeam,
		WorkspaceDefaultTemplate: defaultTemplate,
	}
}

func (c Config) ValidateDatabase() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	return nil
}

func (c Config) ValidateServe() error {
	if err := c.ValidateDatabase(); err != nil {
		return err
	}
	required := map[string]string{
		"E2B_API_KEY":          c.E2BAPIKey,
		"ANTHROPIC_API_KEY":    c.AnthropicAPIKey,
		"SLACK_SIGNING_SECRET": c.SlackSigningSecret,
		"SLACK_BOT_TOKEN":      c.SlackBotToken,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if c.E2BHelperScript == "" {
		return errors.New("E2B_HELPER_SCRIPT is required")
	}
	if c.OpenClawGatewayPort <= 0 {
		return errors.New("OPENCLAW_GATEWAY_PORT must be positive")
	}
	if c.ACPAdapterPort <= 0 {
		return errors.New("E2B_AGENTS_ACP_ADAPTER_PORT must be positive")
	}
	if c.ACPAdapterPort == c.OpenClawGatewayPort {
		return errors.New("E2B_AGENTS_ACP_ADAPTER_PORT must differ from OPENCLAW_GATEWAY_PORT")
	}
	if strings.TrimSpace(c.OpenClawGatewayToken) == "" || c.OpenClawGatewayToken == DefaultOpenClawGatewayToken {
		return errors.New("OPENCLAW_GATEWAY_TOKEN must be set to a non-default secret")
	}
	if c.SandboxTimeout <= 0 {
		return errors.New("E2B_SANDBOX_TIMEOUT must be positive")
	}
	return nil
}

func (c Config) ValidateDevSend(postToSlack bool) error {
	if err := c.ValidateDatabase(); err != nil {
		return err
	}
	if strings.TrimSpace(c.E2BAPIKey) == "" {
		return errors.New("E2B_API_KEY is required")
	}
	if strings.TrimSpace(c.AnthropicAPIKey) == "" {
		return errors.New("ANTHROPIC_API_KEY is required")
	}
	if postToSlack && strings.TrimSpace(c.SlackBotToken) == "" {
		return errors.New("SLACK_BOT_TOKEN is required with --post-to-slack")
	}
	return nil
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func getenvDefault(key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func intDefault(key string, fallback int) int {
	value := getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationDefault(key string, fallback time.Duration) time.Duration {
	value := getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func boolDefault(key string, fallback bool) bool {
	value := strings.ToLower(getenv(key))
	switch value {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
