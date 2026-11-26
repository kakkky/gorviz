package config

import (
	"github.com/kelseyhightower/envconfig"

	"github.com/kakkky/gorviz/internal/protocol/server"
)

// Config の Socket は、--socket を渡さないときにつなぐ server の unix socket
type Config struct {
	Socket string `envconfig:"GORVIZ_SOCKET"`
}

// NewConfig は、Socket がなければ cli と server が揃える決まったパスにする
func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	if cfg.Socket == "" {
		cfg.Socket = server.SocketPath()
	}
	return &cfg, nil
}
