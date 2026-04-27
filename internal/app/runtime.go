package app

import (
	"context"
	"database/sql"

	"github.com/dutifuldev/e2b-agents/internal/config"
	"github.com/dutifuldev/e2b-agents/internal/database"
	"github.com/dutifuldev/e2b-agents/internal/gateway"
	"github.com/dutifuldev/e2b-agents/internal/httpapi"
	"gorm.io/gorm"
)

type Runtime struct {
	cfg     config.Config
	db      *gorm.DB
	sqlDB   *sql.DB
	gateway *gateway.Service
	server  *httpapi.Server
}

func NewRuntime(ctx context.Context, cfg config.Config) (*Runtime, error) {
	db, err := database.Open(cfg.DatabaseURL, database.PoolConfig{
		MaxOpenConns: cfg.DatabaseMaxOpenConns,
		MaxIdleConns: cfg.DatabaseMaxIdleConns,
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, err
	}

	runtimeClient := gateway.NewRuntimeClient(gateway.RuntimeOptions{
		NodePath:       cfg.E2BHelperNode,
		ScriptPath:     cfg.E2BHelperScript,
		APIKey:         cfg.E2BAPIKey,
		AnthropicKey:   cfg.AnthropicAPIKey,
		Model:          cfg.RuntimeModel,
		GatewayPort:    cfg.OpenClawGatewayPort,
		GatewayToken:   cfg.OpenClawGatewayToken,
		Timeout:        cfg.SandboxTimeout,
		RequestTimeout: cfg.SandboxRequestTimeout,
	})
	slackClient := gateway.NewSlackClient(cfg.SlackBotToken)
	gatewayService := gateway.NewService(db, gateway.Options{
		Runtime:           runtimeClient,
		Slack:             slackClient,
		AutoCreate:        cfg.WorkspaceAutoCreate,
		DefaultTeamID:     cfg.WorkspaceDefaultTeamID,
		DefaultTemplate:   cfg.WorkspaceDefaultTemplate,
		ProcessingTimeout: cfg.SlackProcessingTimeout,
	})
	server := httpapi.NewServer(db, httpapi.Options{
		SigningSecret:   cfg.SlackSigningSecret,
		SlackClientID:   cfg.SlackClientID,
		SlackSecret:     cfg.SlackClientSecret,
		SlackRedirect:   cfg.SlackRedirectURL,
		DefaultTeamID:   cfg.WorkspaceDefaultTeamID,
		DefaultTemplate: cfg.WorkspaceDefaultTemplate,
		GatewayService:  gatewayService,
	})
	return &Runtime{
		cfg:     cfg,
		db:      db,
		sqlDB:   sqlDB,
		gateway: gatewayService,
		server:  server,
	}, nil
}

func (r *Runtime) Serve(ctx context.Context) error {
	return r.server.Start(ctx, r.cfg.AppAddr)
}

func (r *Runtime) Gateway() *gateway.Service {
	return r.gateway
}

func (r *Runtime) Close() {
	if r.sqlDB != nil {
		_ = r.sqlDB.Close()
	}
}
