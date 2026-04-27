package database

import (
	"context"

	"gorm.io/gorm"
)

func ApplyMigrations(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(&SlackWorkspace{}, &SlackProcessedEvent{})
}

func ApplyTestSchema(db *gorm.DB) error {
	return ApplyMigrations(context.Background(), db)
}
