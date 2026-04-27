package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dutifuldev/e2b-agents/internal/database"
	"gorm.io/gorm"
)

type Service struct {
	db                *gorm.DB
	workspaces        *WorkspaceService
	runtime           Runtime
	slack             *SlackClient
	autoCreate        bool
	defaultTeamID     string
	defaultTemplate   string
	processingTimeout time.Duration
	locksMu           sync.Mutex
	workspaceLocks    map[string]*sync.Mutex
}

const slackSessionKeyVersion = "v1"

type Options struct {
	Runtime           Runtime
	Slack             *SlackClient
	AutoCreate        bool
	DefaultTeamID     string
	DefaultTemplate   string
	ProcessingTimeout time.Duration
}

type DirectMessageInput struct {
	SlackTeamID string
	ChannelID   string
	UserID      string
	Text        string
	PostToSlack bool
}

type MessageReply struct {
	Text      string
	SandboxID string
	SessionID string
}

type Runtime interface {
	Ensure(context.Context, EnsureRuntimeInput) (EnsureRuntimeOutput, error)
	Send(context.Context, SendRuntimeInput) (SendRuntimeOutput, error)
}

func NewService(db *gorm.DB, opts Options) *Service {
	timeout := opts.ProcessingTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &Service{
		db:                db,
		workspaces:        NewWorkspaceService(db),
		runtime:           opts.Runtime,
		slack:             opts.Slack,
		autoCreate:        opts.AutoCreate,
		defaultTeamID:     opts.DefaultTeamID,
		defaultTemplate:   opts.DefaultTemplate,
		processingTimeout: timeout,
		workspaceLocks:    map[string]*sync.Mutex{},
	}
}

func (s *Service) HandleSlackEnvelope(ctx context.Context, envelope SlackEventEnvelope) {
	ctx, cancel := context.WithTimeout(context.Background(), s.processingTimeout)
	defer cancel()
	if err := s.handleSlackEnvelope(ctx, envelope); err != nil {
		slog.Error("slack event handling failed",
			"event_id", envelope.EventID,
			"team_id", envelope.TeamID,
			"enterprise_id", envelope.EnterpriseID,
			"event_type", envelope.Event.Type,
			"channel_id", envelope.Event.Channel,
			"thread_ts", replyThreadTS(envelope.Event),
			"error", err,
		)
	}
}

func (s *Service) PrewarmReadyWorkspaces(ctx context.Context) error {
	if s.runtime == nil {
		return errors.New("runtime client is not configured")
	}
	var workspaces []database.SlackWorkspace
	if err := s.db.WithContext(ctx).
		Where("setup_status = ? AND current_sandbox_id <> ''", SetupStatusReady).
		Find(&workspaces).Error; err != nil {
		return err
	}
	if len(workspaces) == 0 {
		slog.Info("runtime startup prewarm skipped", "reason", "no_ready_workspaces")
		return nil
	}
	successes := 0
	for _, workspace := range workspaces {
		sessionKey := strings.TrimSpace(workspace.CurrentACPSessionID)
		if sessionKey == "" {
			sessionKey = slackSessionKey(workspace.SlackTeamID, workspace.LastSlackChannelID, prewarmConversationSurface(workspace))
		}
		start := time.Now()
		ensure, err := s.runtime.Ensure(ctx, EnsureRuntimeInput{
			SandboxID:       workspace.CurrentSandboxID,
			TemplateID:      workspace.TemplateID,
			TeamID:          workspace.TeamID,
			RequesterUserID: "startup-prewarm",
			SessionKey:      sessionKey,
			Metadata: map[string]string{
				"ownerType":   "team",
				"ownerId":     workspace.TeamID,
				"teamId":      workspace.TeamID,
				"source":      "startup_prewarm",
				"slackTeamId": workspace.SlackTeamID,
			},
		})
		if err != nil {
			slog.Warn("runtime startup prewarm failed",
				"workspace_id", workspace.ID,
				"slack_team_id", workspace.SlackTeamID,
				"sandbox_id", workspace.CurrentSandboxID,
				"session_id", sessionKey,
				"duration_ms", time.Since(start).Milliseconds(),
				"error", err,
			)
			continue
		}
		sessionID := firstNonEmpty(ensure.SessionKey, sessionKey)
		successes++
		if err := s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
			"current_sandbox_id":     ensure.SandboxID,
			"current_acp_session_id": sessionID,
			"setup_status":           SetupStatusReady,
			"last_error":             "",
		}); err != nil {
			slog.Warn("runtime startup prewarm state update failed",
				"workspace_id", workspace.ID,
				"slack_team_id", workspace.SlackTeamID,
				"sandbox_id", ensure.SandboxID,
				"session_id", sessionID,
				"error", err,
			)
		}
		slog.Info("runtime startup prewarm succeeded",
			"workspace_id", workspace.ID,
			"slack_team_id", workspace.SlackTeamID,
			"sandbox_id", ensure.SandboxID,
			"session_id", sessionID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
	slog.Info("runtime startup prewarm completed",
		"workspace_count", len(workspaces),
		"success_count", successes,
		"failure_count", len(workspaces)-successes,
	)
	return nil
}

func (s *Service) handleSlackEnvelope(ctx context.Context, envelope SlackEventEnvelope) error {
	eventStart := time.Now()
	event := envelope.Event
	if !shouldHandleSlackEvent(event) {
		return nil
	}
	text := strings.TrimSpace(event.Text)
	if text == "" {
		return nil
	}

	workspace, err := s.workspaces.ResolveOrCreate(ctx, firstNonEmpty(envelope.TeamID, event.Team), envelope.EnterpriseID, s.defaultTeamID, s.defaultTemplate, s.autoCreate)
	if err != nil {
		return err
	}

	unlock := s.lockWorkspace(workspace.ID)
	defer unlock()

	workspace, err = s.workspaces.GetBySlackTeamID(ctx, workspace.SlackTeamID)
	if err != nil {
		return err
	}
	claimed, err := s.workspaces.ClaimSlackEvent(ctx, workspace, envelope.EventID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}

	runtimeStart := time.Now()
	reply, err := s.sendToRuntimeLocked(ctx, workspace, event.User, event.Channel, text, sessionConversationID(event))
	runtimeDuration := time.Since(runtimeStart)
	if err != nil {
		_ = s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
			"setup_status": SetupStatusFailed,
			"last_error":   err.Error(),
		})
		if event.Channel != "" {
			postStart := time.Now()
			failurePostErr := s.postWorkspaceMessage(ctx, workspace, event.Channel, replyThreadTS(event), "I could not complete that request. The service recorded the failure for debugging.")
			attrs := []any{
				"workspace_id", workspace.ID,
				"slack_team_id", workspace.SlackTeamID,
				"slack_channel_id", event.Channel,
				"thread_reply", strings.TrimSpace(replyThreadTS(event)) != "",
				"duration_ms", time.Since(postStart).Milliseconds(),
			}
			if failurePostErr != nil {
				attrs = append(attrs, "error", failurePostErr)
			}
			slog.Info("slack failure message post completed", attrs...)
		}
		return err
	}
	var postErr error
	var postDuration time.Duration
	if event.Channel != "" {
		postStart := time.Now()
		postErr = s.postWorkspaceMessage(ctx, workspace, event.Channel, replyThreadTS(event), reply.Text)
		postDuration = time.Since(postStart)
		logLevel := slog.LevelInfo
		if postErr != nil {
			logLevel = slog.LevelWarn
		}
		attrs := []any{
			"workspace_id", workspace.ID,
			"slack_team_id", workspace.SlackTeamID,
			"slack_channel_id", event.Channel,
			"thread_reply", strings.TrimSpace(replyThreadTS(event)) != "",
			"duration_ms", postDuration.Milliseconds(),
		}
		if postErr != nil {
			attrs = append(attrs, "error", postErr)
		}
		slog.Log(ctx, logLevel, "slack reply post completed", attrs...)
	}
	updates := map[string]any{
		"last_slack_event_id":    envelope.EventID,
		"last_slack_channel_id":  event.Channel,
		"last_slack_message_ts":  event.TS,
		"current_sandbox_id":     reply.SandboxID,
		"current_acp_session_id": reply.SessionID,
		"setup_status":           SetupStatusReady,
		"last_activity_at":       time.Now().UTC(),
		"last_error":             "",
	}
	if postErr != nil {
		updates["last_error"] = postErr.Error()
	}
	updateStart := time.Now()
	if err := s.workspaces.UpdateAfterMessage(ctx, workspace.ID, updates); err != nil {
		return err
	}
	updateDuration := time.Since(updateStart)
	slog.Info("slack event handled",
		"workspace_id", workspace.ID,
		"slack_team_id", workspace.SlackTeamID,
		"slack_channel_id", event.Channel,
		"thread_reply", strings.TrimSpace(replyThreadTS(event)) != "",
		"sandbox_id", reply.SandboxID,
		"session_id", reply.SessionID,
		"runtime_duration_ms", runtimeDuration.Milliseconds(),
		"slack_post_duration_ms", postDuration.Milliseconds(),
		"database_update_duration_ms", updateDuration.Milliseconds(),
		"total_duration_ms", time.Since(eventStart).Milliseconds(),
	)
	return postErr
}

func shouldHandleSlackEvent(event SlackEvent) bool {
	if event.Type != "message" && event.Type != "app_mention" {
		return false
	}
	if event.Subtype != "" || event.BotID != "" {
		return false
	}
	if event.Type == "message" && event.ChannelType != "im" {
		return false
	}
	return true
}

func (s *Service) HandleDirectMessage(ctx context.Context, input DirectMessageInput) (MessageReply, error) {
	workspace, err := s.workspaces.ResolveOrCreate(ctx, input.SlackTeamID, "", s.defaultTeamID, s.defaultTemplate, s.autoCreate)
	if err != nil {
		return MessageReply{}, err
	}
	reply, err := s.sendToRuntime(ctx, workspace, input.UserID, input.ChannelID, input.Text, "")
	if err != nil {
		return MessageReply{}, err
	}
	if input.PostToSlack {
		if err := s.postWorkspaceMessage(ctx, workspace, input.ChannelID, "", reply.Text); err != nil {
			return MessageReply{}, err
		}
	}
	return reply, s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
		"current_sandbox_id":     reply.SandboxID,
		"current_acp_session_id": reply.SessionID,
		"last_slack_channel_id":  input.ChannelID,
		"last_activity_at":       time.Now().UTC(),
		"setup_status":           SetupStatusReady,
		"last_error":             "",
	})
}

func (s *Service) sendToRuntime(ctx context.Context, workspace database.SlackWorkspace, requesterUserID, channelID, text, messageTS string) (MessageReply, error) {
	unlock := s.lockWorkspace(workspace.ID)
	defer unlock()
	return s.sendToRuntimeLocked(ctx, workspace, requesterUserID, channelID, text, messageTS)
}

func (s *Service) sendToRuntimeLocked(ctx context.Context, workspace database.SlackWorkspace, requesterUserID, channelID, text, messageTS string) (MessageReply, error) {
	if s.runtime == nil {
		return MessageReply{}, errors.New("runtime client is not configured")
	}

	latest, err := s.workspaces.GetBySlackTeamID(ctx, workspace.SlackTeamID)
	if err != nil {
		return MessageReply{}, err
	}
	workspace = latest

	sessionKey := slackSessionKey(workspace.SlackTeamID, channelID, messageTS)
	if workspaceReadyForDirectSend(workspace) {
		start := time.Now()
		send, err := s.runtime.Send(ctx, SendRuntimeInput{
			SandboxID:  workspace.CurrentSandboxID,
			Prompt:     text,
			SessionKey: sessionKey,
		})
		if err == nil {
			slog.Info("runtime direct send succeeded",
				"workspace_id", workspace.ID,
				"slack_team_id", workspace.SlackTeamID,
				"slack_channel_id", channelID,
				"sandbox_id", workspace.CurrentSandboxID,
				"session_id", firstNonEmpty(send.SessionKey, sessionKey),
				"duration_ms", time.Since(start).Milliseconds(),
			)
			return MessageReply{
				Text:      send.Text,
				SandboxID: workspace.CurrentSandboxID,
				SessionID: firstNonEmpty(send.SessionKey, sessionKey),
			}, nil
		}
		if !isRuntimeUnavailableError(err) {
			slog.Warn("runtime direct send failed without recovery",
				"workspace_id", workspace.ID,
				"slack_team_id", workspace.SlackTeamID,
				"slack_channel_id", channelID,
				"sandbox_id", workspace.CurrentSandboxID,
				"duration_ms", time.Since(start).Milliseconds(),
				"error", err,
			)
			return MessageReply{}, err
		}
		slog.Warn("runtime direct send unavailable; ensuring runtime",
			"workspace_id", workspace.ID,
			"slack_team_id", workspace.SlackTeamID,
			"slack_channel_id", channelID,
			"sandbox_id", workspace.CurrentSandboxID,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
	}

	_ = s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
		"setup_status": SetupStatusCreatingSandbox,
		"last_error":   "",
	})
	ensureStart := time.Now()
	ensure, err := s.runtime.Ensure(ctx, EnsureRuntimeInput{
		SandboxID:       workspace.CurrentSandboxID,
		TemplateID:      workspace.TemplateID,
		TeamID:          workspace.TeamID,
		RequesterUserID: requesterUserID,
		SessionKey:      sessionKey,
		Metadata: map[string]string{
			"ownerType":      "team",
			"ownerId":        workspace.TeamID,
			"teamId":         workspace.TeamID,
			"source":         "slack",
			"slackTeamId":    workspace.SlackTeamID,
			"slackChannelId": channelID,
		},
	})
	if err != nil {
		return MessageReply{}, err
	}
	slog.Info("runtime ensure succeeded",
		"workspace_id", workspace.ID,
		"slack_team_id", workspace.SlackTeamID,
		"slack_channel_id", channelID,
		"sandbox_id", ensure.SandboxID,
		"session_id", ensure.SessionKey,
		"duration_ms", time.Since(ensureStart).Milliseconds(),
	)
	_ = s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
		"setup_status":       SetupStatusWaitingReady,
		"current_sandbox_id": ensure.SandboxID,
	})
	sendStart := time.Now()
	send, err := s.runtime.Send(ctx, SendRuntimeInput{
		SandboxID:  ensure.SandboxID,
		Prompt:     text,
		SessionKey: ensure.SessionKey,
	})
	if err != nil {
		return MessageReply{}, err
	}
	sessionID := firstNonEmpty(send.SessionKey, ensure.SessionKey)
	slog.Info("runtime send after ensure succeeded",
		"workspace_id", workspace.ID,
		"slack_team_id", workspace.SlackTeamID,
		"slack_channel_id", channelID,
		"sandbox_id", ensure.SandboxID,
		"session_id", sessionID,
		"duration_ms", time.Since(sendStart).Milliseconds(),
	)
	return MessageReply{
		Text:      send.Text,
		SandboxID: ensure.SandboxID,
		SessionID: sessionID,
	}, nil
}

func workspaceReadyForDirectSend(workspace database.SlackWorkspace) bool {
	return workspace.SetupStatus == SetupStatusReady && strings.TrimSpace(workspace.CurrentSandboxID) != ""
}

func isRuntimeUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, pattern := range []string{
		"not found",
		"expired",
		"does not exist",
		"connection refused",
		"connection reset",
		"connection aborted",
		"network is unreachable",
		"no such host",
		"fetch failed",
		"gateway not reachable",
		"runtime adapter endpoint not cached",
		"runtime gateway did not become ready",
		"runtime acp adapter did not become ready",
		"connect timeout",
		"connect timed out",
		"i/o timeout",
		"econnrefused",
		"econnreset",
		"enotfound",
		"etimedout",
	} {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	if strings.Contains(msg, "runtime http 502") || strings.Contains(msg, "runtime http 503") ||
		strings.Contains(msg, "runtime adapter http 502") || strings.Contains(msg, "runtime adapter http 503") {
		return true
	}
	if strings.Contains(msg, "sandbox") && (strings.Contains(msg, "404") || strings.Contains(msg, "410")) {
		return true
	}
	return false
}

func (s *Service) lockWorkspace(workspaceID string) func() {
	s.locksMu.Lock()
	lock := s.workspaceLocks[workspaceID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.workspaceLocks[workspaceID] = lock
	}
	s.locksMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func slackSessionKey(slackTeamID, channelID, conversationSurface string) string {
	conversationID := firstNonEmpty(conversationSurface, "direct")
	return safeRuntimeSessionID(fmt.Sprintf("slack-%s-%s-%s-%s", slackSessionKeyVersion, slackTeamID, channelID, conversationID))
}

func safeRuntimeSessionID(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if out == "" || !isASCIIAlphaNumeric(out[0]) {
		out = "s" + out
	}
	if len(out) <= 128 {
		return out
	}
	sum := sha256.Sum256([]byte(out))
	suffix := hex.EncodeToString(sum[:])[:16]
	return out[:111] + "-" + suffix
}

func isASCIIAlphaNumeric(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func (s *Service) postWorkspaceMessage(ctx context.Context, workspace database.SlackWorkspace, channel, threadTSValue, text string) error {
	client := s.slack
	if strings.TrimSpace(workspace.BotTokenRef) != "" {
		token, err := SlackTokenFromRef(workspace.BotTokenRef)
		if err != nil {
			return err
		}
		client = NewSlackClient(token)
	}
	if client == nil {
		return errors.New("slack client is not configured")
	}
	return client.PostMessage(ctx, channel, threadTSValue, text)
}

func replyThreadTS(event SlackEvent) string {
	if strings.TrimSpace(event.ThreadTS) != "" {
		return strings.TrimSpace(event.ThreadTS)
	}
	return ""
}

func sessionConversationID(event SlackEvent) string {
	if isDirectSlackConversation(event) {
		return "direct"
	}
	if strings.TrimSpace(event.ThreadTS) != "" {
		return strings.TrimSpace(event.ThreadTS)
	}
	if strings.TrimSpace(event.Channel) != "" {
		return "channel"
	}
	return ""
}

func prewarmConversationSurface(workspace database.SlackWorkspace) string {
	channelID := strings.TrimSpace(workspace.LastSlackChannelID)
	if channelID == "" || strings.HasPrefix(channelID, "D") {
		return "direct"
	}
	return "channel"
}

func isDirectSlackConversation(event SlackEvent) bool {
	switch strings.TrimSpace(event.ChannelType) {
	case "im", "mpim":
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(event.Channel), "D")
}

func isBotMentionText(text, botUserID string) bool {
	botUserID = strings.TrimSpace(botUserID)
	return botUserID != "" && strings.Contains(text, "<@"+botUserID+">")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func DecodeSlackEnvelope(body []byte) (SlackEventEnvelope, error) {
	var envelope SlackEventEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return SlackEventEnvelope{}, err
	}
	return envelope, nil
}
