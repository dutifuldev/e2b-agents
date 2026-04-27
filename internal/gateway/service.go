package gateway

import (
	"context"
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
	runtime           *RuntimeClient
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
	Runtime           *RuntimeClient
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

func (s *Service) handleSlackEnvelope(ctx context.Context, envelope SlackEventEnvelope) error {
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

	reply, err := s.sendToRuntimeLocked(ctx, workspace, event.User, event.Channel, text, sessionThreadRootTS(event))
	if err != nil {
		_ = s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
			"setup_status": SetupStatusFailed,
			"last_error":   err.Error(),
		})
		if event.Channel != "" {
			_ = s.postWorkspaceMessage(ctx, workspace, event.Channel, replyThreadTS(event), "I could not complete that request. The service recorded the failure for debugging.")
		}
		return err
	}
	var postErr error
	if event.Channel != "" {
		postErr = s.postWorkspaceMessage(ctx, workspace, event.Channel, replyThreadTS(event), reply.Text)
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
	if err := s.workspaces.UpdateAfterMessage(ctx, workspace.ID, updates); err != nil {
		return err
	}
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
	_ = s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
		"setup_status": SetupStatusCreatingSandbox,
		"last_error":   "",
	})
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
	_ = s.workspaces.UpdateAfterMessage(ctx, workspace.ID, map[string]any{
		"setup_status":       SetupStatusWaitingReady,
		"current_sandbox_id": ensure.SandboxID,
	})
	send, err := s.runtime.Send(ctx, SendRuntimeInput{
		SandboxID:  ensure.SandboxID,
		Prompt:     text,
		SessionKey: ensure.SessionKey,
	})
	if err != nil {
		return MessageReply{}, err
	}
	return MessageReply{
		Text:      send.Text,
		SandboxID: ensure.SandboxID,
		SessionID: send.SessionKey,
	}, nil
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

func slackSessionKey(slackTeamID, channelID, threadRootTS string) string {
	conversationID := firstNonEmpty(threadRootTS, "direct")
	return fmt.Sprintf("slack:%s:%s:%s:%s", slackSessionKeyVersion, slackTeamID, channelID, conversationID)
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

func sessionThreadRootTS(event SlackEvent) string {
	if strings.TrimSpace(event.ThreadTS) != "" {
		return event.ThreadTS
	}
	if event.Type == "app_mention" {
		return event.TS
	}
	return ""
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
