package database

import "time"

type SlackWorkspace struct {
	ID                  string `gorm:"primaryKey"`
	TeamID              string `gorm:"index;not null"`
	SlackTeamID         string `gorm:"uniqueIndex;not null"`
	SlackEnterpriseID   string
	SlackTeamName       string
	BotTokenRef         string
	SigningSecretRef    string
	BotUserID           string
	TemplateID          string `gorm:"not null"`
	CurrentSandboxID    string
	CurrentACPSessionID string
	LastSlackEventID    string
	LastSlackChannelID  string
	LastSlackMessageTS  string
	SetupStatus         string `gorm:"not null"`
	LastActivityAt      *time.Time
	LastError           string
	InstalledByUserID   string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (SlackWorkspace) TableName() string {
	return TableSlackWorkspaces
}

type SlackProcessedEvent struct {
	EventID     string `gorm:"primaryKey"`
	WorkspaceID string `gorm:"index;not null"`
	SlackTeamID string `gorm:"index;not null"`
	CreatedAt   time.Time
}

func (SlackProcessedEvent) TableName() string {
	return TableSlackProcessedEvents
}

type SchemaMigration struct {
	Version   int `gorm:"primaryKey"`
	AppliedAt time.Time
}

func (SchemaMigration) TableName() string {
	return TableSchemaMigrations
}
