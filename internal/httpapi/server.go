package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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
	slackClientID  string
	slackSecret    string
	slackRedirect  string
	signingSecret  string
	defaultTeamID  string
	defaultTpl     string
	gatewayService *gateway.Service
}

type Options struct {
	SigningSecret   string
	SlackClientID   string
	SlackSecret     string
	SlackRedirect   string
	DefaultTeamID   string
	DefaultTemplate string
	GatewayService  *gateway.Service
}

const slackMaxBodyBytes int64 = 1 << 20

func NewServer(db *gorm.DB, opts Options) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	s := &Server{
		db:             db,
		echo:           e,
		slackClientID:  strings.TrimSpace(opts.SlackClientID),
		slackSecret:    strings.TrimSpace(opts.SlackSecret),
		slackRedirect:  strings.TrimSpace(opts.SlackRedirect),
		signingSecret:  strings.TrimSpace(opts.SigningSecret),
		defaultTeamID:  strings.TrimSpace(opts.DefaultTeamID),
		defaultTpl:     strings.TrimSpace(opts.DefaultTemplate),
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
	s.echo.GET("/slack/install", s.handleSlackInstall)
	s.echo.GET("/slack/oauth/callback", s.handleSlackOAuthCallback)
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
	body, err := readLimitedBody(c)
	if err != nil {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"message": "Request body too large"})
	}
	if err := gateway.VerifySlackSignature(s.signingSecret, c.Request().Header, body, time.Now()); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"message": "Invalid Slack signature"})
	}
	var challenge struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &challenge); err == nil && challenge.Type == "url_verification" {
		return c.String(http.StatusOK, challenge.Challenge)
	}
	envelope, err := gateway.DecodeSlackEnvelope(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"message": "Invalid Slack event"})
	}
	go s.gatewayService.HandleSlackEnvelope(context.Background(), envelope)
	return c.JSON(http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) handleSlackForm(c echo.Context) error {
	body, err := readLimitedBody(c)
	if err != nil {
		return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"message": "Request body too large"})
	}
	if err := gateway.VerifySlackSignature(s.signingSecret, c.Request().Header, body, time.Now()); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"message": "Invalid Slack signature"})
	}
	return c.JSON(http.StatusNotImplemented, map[string]string{"message": "Slack commands and interactions are not supported yet"})
}

func (s *Server) handleSlackInstall(c echo.Context) error {
	if s.slackClientID == "" || s.slackRedirect == "" {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"message": "Slack OAuth is not configured"})
	}
	state, err := randomState()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"message": "Could not start Slack OAuth"})
	}
	c.SetCookie(&http.Cookie{
		Name:     "slack_oauth_state",
		Value:    state,
		Path:     "/slack/oauth/callback",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	values := url.Values{}
	values.Set("client_id", s.slackClientID)
	values.Set("redirect_uri", s.slackRedirect)
	values.Set("scope", "app_mentions:read,channels:history,chat:write,commands,im:history,im:write")
	values.Set("state", state)
	return c.Redirect(http.StatusFound, "https://slack.com/oauth/v2/authorize?"+values.Encode())
}

func (s *Server) handleSlackOAuthCallback(c echo.Context) error {
	if s.slackClientID == "" || s.slackSecret == "" {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"message": "Slack OAuth is not configured"})
	}
	if slackErr := strings.TrimSpace(c.QueryParam("error")); slackErr != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"message": "Slack OAuth failed"})
	}
	cookie, err := c.Cookie("slack_oauth_state")
	if err != nil || cookie.Value == "" || cookie.Value != c.QueryParam("state") {
		return c.JSON(http.StatusBadRequest, map[string]string{"message": "Invalid Slack OAuth state"})
	}
	c.SetCookie(&http.Cookie{Name: "slack_oauth_state", Value: "", Path: "/slack/oauth/callback", MaxAge: -1})
	code := strings.TrimSpace(c.QueryParam("code"))
	if code == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"message": "Missing Slack OAuth code"})
	}
	access, err := gateway.ExchangeSlackOAuthCode(c.Request().Context(), s.slackClientID, s.slackSecret, s.slackRedirect, code)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"message": "Slack OAuth exchange failed"})
	}
	workspace, err := gateway.NewWorkspaceService(s.db).EnsureWorkspace(c.Request().Context(), gateway.EnsureWorkspaceInput{
		SlackTeamID:       access.TeamID,
		SlackEnterpriseID: access.EnterpriseID,
		SlackTeamName:     access.TeamName,
		TeamID:            s.defaultTeamID,
		TemplateID:        s.defaultTpl,
		BotTokenRef:       "literal:" + access.AccessToken,
		SigningSecretRef:  "env:SLACK_SIGNING_SECRET",
		BotUserID:         access.BotUserID,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"message": "Could not save Slack workspace"})
	}
	return c.String(http.StatusOK, "Slack workspace installed: "+workspace.SlackTeamID)
}

func randomState() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func readLimitedBody(c echo.Context) ([]byte, error) {
	request := c.Request()
	request.Body = http.MaxBytesReader(c.Response().Writer, request.Body, slackMaxBodyBytes)
	return io.ReadAll(request.Body)
}
