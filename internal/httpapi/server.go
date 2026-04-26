package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dutifuldev/e2b-agents/internal/gateway"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"
)

type Server struct {
	db             *gorm.DB
	echo           *echo.Echo
	signingSecret  string
	gatewayService *gateway.Service
}

type Options struct {
	SigningSecret  string
	GatewayService *gateway.Service
}

func NewServer(db *gorm.DB, opts Options) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	s := &Server{
		db:             db,
		echo:           e,
		signingSecret:  strings.TrimSpace(opts.SigningSecret),
		gatewayService: opts.GatewayService,
	}
	s.registerRoutes()
	return s
}

func (s *Server) Start(ctx context.Context, addr string) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.echo.Start(addr)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.echo.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) Echo() *echo.Echo {
	return s.echo
}

func (s *Server) registerRoutes() {
	s.echo.GET("/healthz", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	s.echo.GET("/readyz", s.handleReady)
	s.echo.GET("/slack/install", s.notImplemented)
	s.echo.GET("/slack/oauth/callback", s.notImplemented)
	s.echo.POST("/slack/events", s.handleSlackEvents)
	s.echo.POST("/slack/interactions", s.handleSlackForm)
	s.echo.POST("/slack/commands", s.handleSlackForm)
}

func (s *Server) handleReady(c echo.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"message": "database unavailable"})
	}
	if err := sqlDB.PingContext(c.Request().Context()); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"message": "database unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleSlackEvents(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return err
	}
	if err := gateway.VerifySlackSignature(s.signingSecret, c.Request().Header, body, time.Now()); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"message": "Invalid Slack signature"})
	}
	var challenge struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &challenge); err == nil && challenge.Type == "url_verification" {
		return c.JSON(http.StatusOK, map[string]string{"challenge": challenge.Challenge})
	}
	envelope, err := gateway.DecodeSlackEnvelope(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"message": "Invalid Slack event"})
	}
	go s.gatewayService.HandleSlackEnvelope(context.Background(), envelope)
	return c.JSON(http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) handleSlackForm(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return err
	}
	if err := gateway.VerifySlackSignature(s.signingSecret, c.Request().Header, body, time.Now()); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"message": "Invalid Slack signature"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) notImplemented(c echo.Context) error {
	return c.JSON(http.StatusNotImplemented, map[string]string{"message": "Not Implemented"})
}
