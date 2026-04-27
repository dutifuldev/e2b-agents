package database

import "time"

type SlackWorkspace struct {
	ID                  string     `gorm:"primaryKey"`
	TeamID              string     `gorm:"index;not null"`
	SlackTeamID         string     `gorm:"uniqueIndex;not null"`
	SlackEnterpriseID   string     `gorm:"not null;default:''"`
	SlackTeamName       string     `gorm:"not null;default:''"`
	BotTokenRef         string     `gorm:"not null;default:''"`
	SigningSecretRef    string     `gorm:"not null;default:''"`
	BotUserID           string     `gorm:"not null;default:''"`
	TemplateID          string     `gorm:"not null"`
	CurrentSandboxID    string     `gorm:"index;not null;default:''"`
	CurrentACPSessionID string     `gorm:"not null;default:''"`
	LastSlackEventID    string     `gorm:"not null;default:''"`
	LastSlackChannelID  string     `gorm:"not null;default:''"`
	LastSlackMessageTS  string     `gorm:"not null;default:''"`
	SetupStatus         string     `gorm:"not null;default:'ready'"`
	LastActivityAt      *time.Time `gorm:"index"`
	LastError           string     `gorm:"not null;default:''"`
	InstalledByUserID   string     `gorm:"not null;default:''"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (SlackWorkspace) TableName() string {
	return TableSlackWorkspaces
}

type SlackProcessedEvent struct {
	EventID     string    `gorm:"primaryKey"`
	WorkspaceID string    `gorm:"index;not null"`
	SlackTeamID string    `gorm:"index;not null"`
	CreatedAt   time.Time `gorm:"index"`
}

func (SlackProcessedEvent) TableName() string {
	return TableSlackProcessedEvents
}
