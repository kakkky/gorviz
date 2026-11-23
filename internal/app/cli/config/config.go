package config

import "github.com/kelseyhightower/envconfig"

type Config struct {
	ServerURL     string `envconfig:"GORVIZ_SERVER_URL" default:"https://localhost:7070"`
	Token         string `envconfig:"GORVIZ_TOKEN"`
	CAFile        string `envconfig:"GORVIZ_CA_FILE"`
	CAFingerprint string `envconfig:"GORVIZ_CA_FINGERPRINT"`
}

func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
