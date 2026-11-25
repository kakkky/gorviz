package config

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Addr          string        `envconfig:"TODO_ADDR" default:"localhost:8090"`
	BackupDir     string        `envconfig:"TODO_BACKUP_DIR" default:"/tmp"`
	BackupEvery   time.Duration `envconfig:"TODO_BACKUP_EVERY" default:"1m"`
	ReminderLimit time.Duration `envconfig:"TODO_REMINDER_TIMEOUT" default:"1s"`
}

func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
