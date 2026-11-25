package config

import "github.com/kelseyhightower/envconfig"

// Config の Addr は、--addr を渡さないときにつなぐ server のアドレス
type Config struct {
	Addr string `envconfig:"GORVIZ_ADDR" default:"127.0.0.1:7070"`
}

func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
