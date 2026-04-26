package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dutifuldev/e2b-agents/internal/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SetupStatusReady           = "ready"
	SetupStatusCreatingSandbox = "creating_sandbox"
	SetupStatusWaitingReady    = "waiting_ready"
	SetupStatusFailed          = "failed"
)

type WorkspaceService struct {
	db *gorm.DB
}

type EnsureWorkspaceInput struct {
	SlackTeamID       string
	SlackEnterpriseID string
	SlackTeamName     string
	TeamID            string
	TemplateID        string
	BotTokenRef       string
	SigningSecretRef  string
	BotUserID         string
}

func NewWorkspaceService(db *gorm.DB) *WorkspaceService {
	return &WorkspaceService{db: db}
}

func (s *WorkspaceService) EnsureWorkspace(ctx context.Context, input EnsureWorkspaceInput) (database.SlackWorkspace, error) {
	input.SlackTeamID = strings.TrimSpace(input.SlackTeamID)
	input.TeamID = strings.TrimSpace(input.TeamID)
	input.TemplateID = strings.TrimSpace(input.TemplateID)
	if input.SlackTeamID == "" {
		return database.SlackWorkspace{}, errors.New("slack team ID is required")
	}
	if input.TeamID == "" {
		return database.SlackWorkspace{}, errors.New("team ID is required")
	}
	if input.TemplateID == "" {
		return database.SlackWorkspace{}, errors.New("template ID is required")
	}
	if input.BotTokenRef == "" {
		input.BotTokenRef = "env:SLACK_BOT_TOKEN"
	}
	if input.SigningSecretRef == "" {
		input.SigningSecretRef = "env:SLACK_SIGNING_SECRET"
	}

	now := time.Now().UTC()
	workspace := database.SlackWorkspace{
		ID:                "slack_" + input.SlackTeamID,
		SlackTeamID:       input.SlackTeamID,
		TeamID:            input.TeamID,
		SlackEnterpriseID: input.SlackEnterpriseID,
		SlackTeamName:     input.SlackTeamName,
		BotTokenRef:       input.BotTokenRef,
		SigningSecretRef:  input.SigningSecretRef,
		BotUserID:         input.BotUserID,
		TemplateID:        input.TemplateID,
		SetupStatus:       SetupStatusReady,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.db.WithContext(ctx).Where("slack_team_id = ?", input.SlackTeamID).FirstOrCreate(&workspace).Error; err != nil {
		existing, getErr := s.GetBySlackTeamID(ctx, input.SlackTeamID)
		if getErr != nil {
			return database.SlackWorkspace{}, err
		}
		workspace = existing
	}
	updates := map[string]any{
		"team_id":             input.TeamID,
		"slack_enterprise_id": input.SlackEnterpriseID,
		"slack_team_name":     input.SlackTeamName,
		"bot_token_ref":       input.BotTokenRef,
		"signing_secret_ref":  input.SigningSecretRef,
		"bot_user_id":         input.BotUserID,
		"template_id":         input.TemplateID,
		"updated_at":          now,
	}
	if workspace.TemplateID != "" && workspace.TemplateID != input.TemplateID {
		updates["current_sandbox_id"] = ""
		updates["current_acp_session_id"] = ""
		updates["setup_status"] = SetupStatusReady
		updates["last_error"] = ""
	}
	if err := s.db.WithContext(ctx).Model(&workspace).Updates(updates).Error; err != nil {
		return database.SlackWorkspace{}, err
	}
	if err := s.db.WithContext(ctx).Where("slack_team_id = ?", input.SlackTeamID).First(&workspace).Error; err != nil {
		return database.SlackWorkspace{}, err
	}
	return workspace, nil
}

func (s *WorkspaceService) GetBySlackTeamID(ctx context.Context, slackTeamID string) (database.SlackWorkspace, error) {
	var workspace database.SlackWorkspace
	err := s.db.WithContext(ctx).Where("slack_team_id = ?", strings.TrimSpace(slackTeamID)).First(&workspace).Error
	return workspace, err
}

func (s *WorkspaceService) UpdateAfterMessage(ctx context.Context, workspaceID string, updates map[string]any) error {
	updates["updated_at"] = time.Now().UTC()
	return s.db.WithContext(ctx).Model(&database.SlackWorkspace{}).Where("id = ?", workspaceID).Updates(updates).Error
}

func (s *WorkspaceService) IsSlackEventProcessed(ctx context.Context, eventID string) (bool, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&database.SlackProcessedEvent{}).Where("event_id = ?", eventID).Count(&count).Error
	return count > 0, err
}

func (s *WorkspaceService) MarkSlackEventProcessed(ctx context.Context, workspace database.SlackWorkspace, eventID string) error {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	event := database.SlackProcessedEvent{
		EventID:     eventID,
		WorkspaceID: workspace.ID,
		SlackTeamID: workspace.SlackTeamID,
		CreatedAt:   time.Now().UTC(),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error
}

func (s *WorkspaceService) ResolveOrCreate(ctx context.Context, slackTeamID, enterpriseID, defaultTeamID, defaultTemplateID string, autoCreate bool) (database.SlackWorkspace, error) {
	workspace, err := s.GetBySlackTeamID(ctx, slackTeamID)
	if err == nil {
		return workspace, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return database.SlackWorkspace{}, err
	}
	if !autoCreate {
		return database.SlackWorkspace{}, fmt.Errorf("slack workspace %s is not configured", slackTeamID)
	}
	return s.EnsureWorkspace(ctx, EnsureWorkspaceInput{
		SlackTeamID:       slackTeamID,
		SlackEnterpriseID: enterpriseID,
		TeamID:            defaultTeamID,
		TemplateID:        defaultTemplateID,
	})
}
