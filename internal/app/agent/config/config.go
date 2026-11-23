package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

const (
	ModeSidecar = "sidecar"
	ModeDaemon  = "daemon"
	ModeLibrary = "library"
)

type Config struct {
	Mode string `envconfig:"GORVIZ_AGENT_MODE" default:"sidecar"`

	ServerAddr    string `envconfig:"GORVIZ_SERVER_ADDR" default:"localhost:7071"`
	Insecure      bool   `envconfig:"GORVIZ_INSECURE" default:"false"`
	CAFile        string `envconfig:"GORVIZ_CA_FILE"`
	CAFingerprint string `envconfig:"GORVIZ_CA_FINGERPRINT"`
	Token         string `envconfig:"GORVIZ_TOKEN"`

	Service   string `envconfig:"GORVIZ_SERVICE"`
	Instance  string `envconfig:"GORVIZ_INSTANCE"`
	PprofAddr string `envconfig:"GORVIZ_PPROF_ADDR"`

	Stdin            bool          `envconfig:"GORVIZ_LOG_STDIN" default:"false"`
	StdinEcho        bool          `envconfig:"GORVIZ_LOG_STDIN_ECHO" default:"false"`
	LogFiles         []string      `envconfig:"GORVIZ_LOG_FILES"`
	LogTCPAddr       string        `envconfig:"GORVIZ_LOG_TCP_ADDR"`
	LogUDPAddr       string        `envconfig:"GORVIZ_LOG_UDP_ADDR"`
	LogPollInterval  time.Duration `envconfig:"GORVIZ_LOG_POLL_INTERVAL" default:"500ms"`
	LogMaxLineBytes  int           `envconfig:"GORVIZ_LOG_MAX_LINE_BYTES" default:"1048576"`
	LogQueueLength   int           `envconfig:"GORVIZ_LOG_QUEUE_LENGTH" default:"4096"`
	LogDurationKeys  []string      `envconfig:"GORVIZ_LOG_DURATION_KEYS" default:"duration,latency,elapsed"`
	PodLogDir        string        `envconfig:"GORVIZ_POD_LOG_DIR" default:"/var/log/pods"`
	NodeName         string        `envconfig:"GORVIZ_NODE_NAME"`
	KubernetesAPI    string        `envconfig:"GORVIZ_KUBERNETES_API" default:"https://kubernetes.default.svc"`
	KubernetesToken  string        `envconfig:"GORVIZ_KUBERNETES_TOKEN_FILE" default:"/var/run/secrets/kubernetes.io/serviceaccount/token"`
	KubernetesCAFile string        `envconfig:"GORVIZ_KUBERNETES_CA_FILE" default:"/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"`

	ChunkLength       time.Duration `envconfig:"GORVIZ_CHUNK_LENGTH" default:"5s"`
	LeakCheckInterval time.Duration `envconfig:"GORVIZ_LEAK_CHECK_INTERVAL" default:"1m"`
	SyncInterval      time.Duration `envconfig:"GORVIZ_SYNC_INTERVAL" default:"10s"`
	ExportInterval    time.Duration `envconfig:"GORVIZ_EXPORT_INTERVAL" default:"3s"`
	RetryInterval     time.Duration `envconfig:"GORVIZ_RETRY_INTERVAL" default:"1s"`
	RequestTimeout    time.Duration `envconfig:"GORVIZ_REQUEST_TIMEOUT" default:"10s"`
	BufferBytes       int           `envconfig:"GORVIZ_BUFFER_BYTES" default:"67108864"`
	// FlightRecorderBytes はライブラリ型で、アプリのメモリに持つ直近の trace の上限
	FlightRecorderBytes int `envconfig:"GORVIZ_FLIGHT_RECORDER_BYTES" default:"16777216"`

	// AgentID は環境変数から読まず、起動ごとに作る
	AgentID observation.AgentID `ignored:"true"`
}

func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	cfg.AgentID = observation.AgentID(rand.Text())
	return &cfg, nil
}

// Validate は置き方ごとに要るものを確かめる。launcher が NewConfig の後に上書きするので、NewConfig の中では確かめない
func (c *Config) Validate() error {
	var errs []error
	switch c.Mode {
	case ModeSidecar:
		if c.PprofAddr == "" {
			errs = append(errs, errors.New("GORVIZ_PPROF_ADDR is required in the sidecar mode"))
		}
	case ModeDaemon:
		if c.NodeName == "" {
			errs = append(errs, errors.New("GORVIZ_NODE_NAME is required in the daemon mode"))
		}
	case ModeLibrary:
	default:
		errs = append(errs, fmt.Errorf("GORVIZ_AGENT_MODE must be sidecar or daemon, got %q", c.Mode))
	}
	if c.Mode != ModeDaemon && c.Service == "" {
		errs = append(errs, errors.New("GORVIZ_SERVICE is required"))
	}
	if c.ServerAddr == "" {
		errs = append(errs, errors.New("GORVIZ_SERVER_ADDR is required"))
	}
	if c.AgentID == "" {
		errs = append(errs, errors.New("the agent ID is empty; build the config with NewConfig"))
	}
	if c.ChunkLength <= 0 || c.ExportInterval <= 0 || c.SyncInterval <= 0 || c.LeakCheckInterval < 0 {
		errs = append(errs, errors.New("the intervals must be positive (the leak check interval may be 0 to disable it)"))
	}
	if c.BufferBytes <= 0 || c.LogQueueLength <= 0 || c.LogMaxLineBytes <= 0 {
		errs = append(errs, errors.New("GORVIZ_BUFFER_BYTES, GORVIZ_LOG_QUEUE_LENGTH and GORVIZ_LOG_MAX_LINE_BYTES must be positive"))
	}
	return errors.Join(errs...)
}
