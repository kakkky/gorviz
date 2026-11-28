package config

import (
	"os"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
)

// Config の Socket は、--socket を渡さないときにつなぐ server の unix socket。ServerLog は起動した server の出力の書き出し先
type Config struct {
	Socket    string `envconfig:"GORVIZ_SOCKET"`
	ServerLog string
}

// NewConfig は、Socket がなければ決まったパスにする
func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	dir := filepath.Join(os.TempDir(), "gorviz")
	if cfg.Socket == "" {
		cfg.Socket = filepath.Join(dir, "server.sock")
	}
	cfg.ServerLog = filepath.Join(dir, "server.log")
	return &cfg, nil
}
