// Package server は server との決まり。server を起動するときの環境変数、HTTP API につなぐ socket、HTTP API のエンドポイント。両側 (cli と server) が揃えるものだけを置く
package server

import (
	"os"
	"path/filepath"
)

// cli が server を起動するときに渡し、server が読む環境変数
const (
	EnvServer     = "GORVIZ_SERVER"
	EnvSocket     = "GORVIZ_SOCKET"
	EnvAppDir     = "GORVIZ_APP_DIR"
	EnvAppPackage = "GORVIZ_APP_PACKAGE"
	EnvAppArgs    = "GORVIZ_APP_ARGS"
)

// ServerEnvs は server がアプリに渡さない環境変数
var ServerEnvs = []string{EnvServer, EnvSocket, EnvAppDir, EnvAppPackage, EnvAppArgs}

// IsServer は、cli に server として起動されたか
func IsServer() bool {
	return os.Getenv(EnvServer) == "1"
}

// SocketPath は、cli が server の HTTP API につなぐ unix socket の決まったパス。cli と server は同じマシンの中なので、ネットワークを通さない
func SocketPath() string {
	return filepath.Join(os.TempDir(), "gorviz", "server.sock")
}

// LogPath は server の出力の書き出し先
func LogPath() string {
	return filepath.Join(os.TempDir(), "gorviz", "server.log")
}
