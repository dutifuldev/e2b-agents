package database

import (
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type PoolConfig struct {
	MaxOpenConns int
	MaxIdleConns int
}

func Open(databaseURL string, pool PoolConfig) (*gorm.DB, error) {
	db, err := gorm.Open(dialector(databaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if pool.MaxOpenConns <= 0 {
		pool.MaxOpenConns = 10
	}
	if pool.MaxIdleConns <= 0 {
		pool.MaxIdleConns = 5
	}
	if pool.MaxIdleConns > pool.MaxOpenConns {
		pool.MaxIdleConns = pool.MaxOpenConns
	}
	sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)
	sqlDB.SetConnMaxLifetime(2 * time.Hour)
	return db, nil
}

func dialector(databaseURL string) gorm.Dialector {
	if strings.HasPrefix(databaseURL, "sqlite://") {
		return sqlite.Open(strings.TrimPrefix(databaseURL, "sqlite://"))
	}
	if databaseURL == ":memory:" {
		return sqlite.Open(databaseURL)
	}
	return postgres.Open(databaseURL)
}
