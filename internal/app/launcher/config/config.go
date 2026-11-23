// Package config は launcher の設定。人が手元で 1 行で打つ入口なので、環境変数ではなくフラグで読む
package config

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"net"
	"os"

	agentconfig "github.com/kakkky/gorviz/internal/app/agent/config"
	serverconfig "github.com/kakkky/gorviz/internal/app/server/config"
)

type Config struct {
	Server *serverconfig.Config
	Agent  *agentconfig.Config
	GUIURL string
}

func NewConfig() (*Config, error) {
	return parse(os.Args[1:], stdinIsPiped())
}

func parse(args []string, piped bool) (*Config, error) {
	server, err := serverconfig.NewConfig()
	if err != nil {
		return nil, err
	}
	agent, err := agentconfig.NewConfig()
	if err != nil {
		return nil, err
	}
	fs := flag.NewFlagSet("gorviz-launcher", flag.ContinueOnError)
	pprofAddr := fs.String("pprof-addr", "", "観測するアプリの pprof のアドレス (例 localhost:6060)")
	service := fs.String("service", "app", "サービスの名前")
	dataDir := fs.String("data-dir", ".gorviz", "server の保存先")
	httpAddr := fs.String("http-addr", "localhost:7070", "GUI と HTTP API の待ち受け")
	agentAddr := fs.String("agent-addr", "localhost:7071", "agent からの接続の待ち受け")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *pprofAddr == "" {
		fs.Usage()
		return nil, flag.ErrHelp
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	guiURL := "http://" + browserAddr(*httpAddr)

	server.DataDir = *dataDir
	server.AgentAddr = *agentAddr
	server.HTTPAddr = *httpAddr
	server.Insecure = true
	server.BootstrapToken = token
	server.PublicURL = guiURL

	agent.Mode = "sidecar"
	agent.ServerAddr = *agentAddr
	agent.Insecure = true
	agent.Token = token
	agent.Service = *service
	agent.PprofAddr = *pprofAddr
	agent.Stdin = piped
	agent.StdinEcho = piped

	return &Config{Server: server, Agent: agent, GUIURL: guiURL}, nil
}

func stdinIsPiped() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice == 0
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// browserAddr は ":7070" のように host を省いた待ち受けを、ブラウザで開ける形にする
func browserAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "" {
		return addr
	}
	return net.JoinHostPort("localhost", port)
}
