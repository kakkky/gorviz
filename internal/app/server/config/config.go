package config

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Args はアプリの引数。空白を含められるよう、JSON の配列で受け取る
type Args []string

func (a *Args) Decode(v string) error {
	return json.Unmarshal([]byte(v), (*[]string)(a))
}

type Config struct {
	Socket          string        `envconfig:"GORVIZ_SOCKET" required:"true"`
	ServerLog       string        `envconfig:"GORVIZ_SERVER_LOG" required:"true"`
	GUIAddr         string        `envconfig:"GORVIZ_GUI_ADDR" default:"127.0.0.1:24678"`
	AppDir          string        `envconfig:"GORVIZ_APP_DIR" required:"true"`
	AppPackage      string        `envconfig:"GORVIZ_APP_PACKAGE" required:"true"`
	AppArgs         Args          `envconfig:"GORVIZ_APP_ARGS" default:"[]"`
	ShutdownTimeout time.Duration `envconfig:"GORVIZ_SHUTDOWN_TIMEOUT" default:"10s"`
}

func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// IsServer は、cli に server として起動されたか
func IsServer() bool {
	return os.Getenv("GORVIZ_SERVER") == "1"
}

// AppEnv は gorviz run の環境変数から、gorviz のもの (GORVIZ_ で始まるもの) を除いたもの。ビルドにもアプリにもそのまま渡す
func (c *Config) AppEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GORVIZ_") {
			out = append(out, kv)
		}
	}
	return out
}
