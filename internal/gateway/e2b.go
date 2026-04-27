package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type RuntimeClient struct {
	nodePath       string
	scriptPath     string
	apiKey         string
	anthropicKey   string
	model          string
	gatewayPort    int
	gatewayToken   string
	timeout        time.Duration
	requestTimeout time.Duration
}

type RuntimeOptions struct {
	NodePath       string
	ScriptPath     string
	APIKey         string
	AnthropicKey   string
	Model          string
	GatewayPort    int
	GatewayToken   string
	Timeout        time.Duration
	RequestTimeout time.Duration
}

type EnsureRuntimeInput struct {
	SandboxID       string            `json:"sandboxId,omitempty"`
	TemplateID      string            `json:"templateId"`
	TeamID          string            `json:"teamId"`
	RequesterUserID string            `json:"requesterUserId"`
	SessionKey      string            `json:"sessionKey"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type EnsureRuntimeOutput struct {
	SandboxID  string `json:"sandboxId"`
	TemplateID string `json:"templateId"`
	Host       string `json:"host"`
	BaseURL    string `json:"baseUrl"`
	SessionKey string `json:"sessionKey"`
}

type SendRuntimeInput struct {
	SandboxID  string `json:"sandboxId"`
	Prompt     string `json:"prompt"`
	SessionKey string `json:"sessionKey"`
}

type SendRuntimeOutput struct {
	Text       string `json:"text"`
	SessionKey string `json:"sessionKey"`
}

func NewRuntimeClient(opts RuntimeOptions) *RuntimeClient {
	if opts.NodePath == "" {
		opts.NodePath = "node"
	}
	if opts.Model == "" {
		opts.Model = "anthropic/claude-sonnet-4-6"
	}
	if opts.GatewayPort <= 0 {
		opts.GatewayPort = 18789
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 5 * time.Minute
	}
	return &RuntimeClient{
		nodePath:       opts.NodePath,
		scriptPath:     opts.ScriptPath,
		apiKey:         opts.APIKey,
		anthropicKey:   opts.AnthropicKey,
		model:          opts.Model,
		gatewayPort:    opts.GatewayPort,
		gatewayToken:   opts.GatewayToken,
		timeout:        opts.Timeout,
		requestTimeout: opts.RequestTimeout,
	}
}

func (c *RuntimeClient) Ensure(ctx context.Context, input EnsureRuntimeInput) (EnsureRuntimeOutput, error) {
	var out EnsureRuntimeOutput
	err := c.run(ctx, "ensure", input, &out)
	return out, err
}

func (c *RuntimeClient) Send(ctx context.Context, input SendRuntimeInput) (SendRuntimeOutput, error) {
	var out SendRuntimeOutput
	err := c.run(ctx, "send", input, &out)
	out.Text = NormalizeSlackText(out.Text)
	return out, err
}

func (c *RuntimeClient) run(ctx context.Context, command string, input any, out any) error {
	if c.scriptPath == "" {
		return errors.New("runtime helper script path is not configured")
	}
	if c.apiKey == "" {
		return errors.New("E2B API key is not configured")
	}
	if c.anthropicKey == "" {
		return errors.New("Anthropic API key is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	envelope := map[string]any{
		"command":          command,
		"input":            input,
		"model":            c.model,
		"gatewayPort":      c.gatewayPort,
		"gatewayToken":     c.gatewayToken,
		"sandboxTimeoutMs": int64(c.timeout / time.Millisecond),
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, c.nodePath, c.scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(cmd.Environ(),
		"E2B_API_KEY="+c.apiKey,
		"ANTHROPIC_API_KEY="+c.anthropicKey,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		emitRuntimeHelperLogs(command, stderr.String())
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		slog.Warn("runtime helper process failed",
			"command", command,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", sanitizeHelperError(msg),
		)
		return fmt.Errorf("runtime helper %s failed: %s", command, sanitizeHelperError(msg))
	}
	emitRuntimeHelperLogs(command, stderr.String())
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("decode runtime helper %s response: %w: %s", command, err, strings.TrimSpace(stdout.String()))
	}
	slog.Info("runtime helper process succeeded",
		"command", command,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return nil
}

func sanitizeHelperError(msg string) string {
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	return msg
}

func emitRuntimeHelperLogs(command, stderrText string) {
	for _, line := range strings.Split(stderrText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			slog.Info("runtime helper log",
				"command", command,
				"message", sanitizeHelperError(line),
			)
			continue
		}
		msg, _ := fields["msg"].(string)
		if msg == "" {
			msg = "runtime helper timing"
		}
		delete(fields, "msg")
		attrs := []any{"command", command}
		for key, value := range fields {
			attrs = append(attrs, key, value)
		}
		slog.Info(msg, attrs...)
	}
}
