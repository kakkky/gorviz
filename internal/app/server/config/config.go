// Package config は gorviz-server の設定を環境変数から読む
package config

import (
	"encoding/json"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// Notifications はサービスの名前から通知の送り先の並びへの対応。環境変数には JSON で書く。
// 例: {"checkout":[{"kind":"slack","url":"https://hooks.slack.com/..."},{"kind":"mail","smtpAddr":"smtp:587","from":"a@x","to":["b@x"]}]}
type Notifications map[observation.ServiceName][]domain.Destination

func (n *Notifications) Decode(v string) error {
	if v == "" {
		*n = Notifications{}
		return nil
	}
	return json.Unmarshal([]byte(v), n)
}

type Config struct {
	DataDir        string `envconfig:"GORVIZ_DATA_DIR" default:"/var/lib/gorviz"`
	AgentAddr      string `envconfig:"GORVIZ_AGENT_ADDR" default:":7071"`
	HTTPAddr       string `envconfig:"GORVIZ_HTTP_ADDR" default:":7070"`
	Insecure       bool   `envconfig:"GORVIZ_INSECURE" default:"false"`
	BootstrapToken string `envconfig:"GORVIZ_BOOTSTRAP_TOKEN" default:""`
	PublicURL      string `envconfig:"GORVIZ_PUBLIC_URL" default:"https://localhost:7070"`

	TLSCertFile string   `envconfig:"GORVIZ_TLS_CERT_FILE" default:""`
	TLSKeyFile  string   `envconfig:"GORVIZ_TLS_KEY_FILE" default:""`
	TLSHosts    []string `envconfig:"GORVIZ_TLS_HOSTS" default:"localhost,127.0.0.1,::1"`

	RetentionMetrics                        time.Duration `envconfig:"GORVIZ_RETENTION_METRICS" default:"744h"`
	RetentionIssues                         time.Duration `envconfig:"GORVIZ_RETENTION_ISSUES" default:"744h"`
	RetentionLogs                           time.Duration `envconfig:"GORVIZ_RETENTION_LOGS" default:"744h"`
	RetentionLogWriterGoroutines            time.Duration `envconfig:"GORVIZ_RETENTION_LOG_WRITER_GOROUTINES" default:"744h"`
	RetentionInstanceLifecycle              time.Duration `envconfig:"GORVIZ_RETENTION_INSTANCE_LIFECYCLE" default:"744h"`
	RetentionExecutionTraces                time.Duration `envconfig:"GORVIZ_RETENTION_EXECUTION_TRACES" default:"168h"`
	RetentionExecutionTracesAroundIssues    time.Duration `envconfig:"GORVIZ_RETENTION_EXECUTION_TRACES_AROUND_ISSUES" default:"744h"`
	RetentionExecutionTracesAroundErrorLogs time.Duration `envconfig:"GORVIZ_RETENTION_EXECUTION_TRACES_AROUND_ERROR_LOGS" default:"744h"`
	RetentionGoroutineSnapshots             time.Duration `envconfig:"GORVIZ_RETENTION_GOROUTINE_SNAPSHOTS" default:"744h"`
	RetentionGoroutineLeakChecks            time.Duration `envconfig:"GORVIZ_RETENTION_GOROUTINE_LEAK_CHECKS" default:"744h"`
	RetentionGoroutineStateBreakdowns       time.Duration `envconfig:"GORVIZ_RETENTION_GOROUTINE_STATE_BREAKDOWNS" default:"744h"`
	// MaxDiskBytes は保存先全体の容量の上限。0 なら上限なし
	MaxDiskBytes int64 `envconfig:"GORVIZ_MAX_DISK_BYTES" default:"107374182400"`

	AggregateMetricsInterval                       time.Duration `envconfig:"GORVIZ_AGGREGATE_METRICS_INTERVAL" default:"10s"`
	AggregateGoroutineStateBreakdownsInterval      time.Duration `envconfig:"GORVIZ_AGGREGATE_GOROUTINE_STATE_BREAKDOWNS_INTERVAL" default:"1m"`
	DetectIssuesInterval                           time.Duration `envconfig:"GORVIZ_DETECT_ISSUES_INTERVAL" default:"5s"`
	PreserveExecutionTracesAroundErrorLogsInterval time.Duration `envconfig:"GORVIZ_PRESERVE_EXECUTION_TRACES_AROUND_ERROR_LOGS_INTERVAL" default:"5s"`
	AttachLogWriterGoroutinesInterval              time.Duration `envconfig:"GORVIZ_ATTACH_LOG_WRITER_GOROUTINES_INTERVAL" default:"5s"`
	PruneDataInterval                              time.Duration `envconfig:"GORVIZ_PRUNE_DATA_INTERVAL" default:"10m"`

	Notifications Notifications `envconfig:"GORVIZ_NOTIFICATIONS" default:""`
}

func NewConfig() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
