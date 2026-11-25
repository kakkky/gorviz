// Package serverapi は、server をデーモンとして起動する決まりと、server の HTTP API のルートとクライアント
package serverapi

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// DefaultAddr は server が待ち受け、cli がつなぐ決まったアドレス
const DefaultAddr = "127.0.0.1:7070"

// デーモンとして起動された server が読む環境変数
const (
	EnvDaemon     = "GORVIZ_DAEMON"
	EnvAddr       = "GORVIZ_ADDR"
	EnvAppDir     = "GORVIZ_APP_DIR"
	EnvAppPackage = "GORVIZ_APP_PACKAGE"
	EnvAppArgs    = "GORVIZ_APP_ARGS"
)

// DaemonEnvs は server がアプリに渡さない環境変数
var DaemonEnvs = []string{EnvDaemon, EnvAddr, EnvAppDir, EnvAppPackage, EnvAppArgs}

func IsDaemon() bool {
	return os.Getenv(EnvDaemon) == "1"
}

// LogPath はデーモンの出力の書き出し先
func LogPath() string {
	return filepath.Join(os.TempDir(), "gorviz", "server.log")
}

// StartDaemon は今の実行ファイルを server として、端末から切り離して起動する。環境変数は gorviz run のものをそのまま渡す
func StartDaemon(addr, dir, pkg string, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(LogPath()), 0o755); err != nil {
		return err
	}
	log, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(exe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), EnvDaemon+"=1", EnvAddr+"="+addr, EnvAppDir+"="+dir, EnvAppPackage+"="+pkg, EnvAppArgs+"="+string(argsJSON))
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
