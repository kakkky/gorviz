// Package config は gorviz-launcher の flag
package config

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	agentconfig "github.com/kakkky/gorviz/internal/app/agent/config"
	serverconfig "github.com/kakkky/gorviz/internal/app/server/config"
)

type Config struct {
	Server *serverconfig.Config
	Agent  *agentconfig.Config
	// TargetCommand は -- の後ろに書いた、対象のアプリを起動するコマンド。空なら対象は起動しない
	TargetCommand []string
}

// NewConfig は -addr・-agent-addr・-save-dir を server に、残りのフラグを agent に渡す。-agent-addr は両方に渡す。
// -- の後ろは対象を起動するコマンドで、その出力を agent がログとして取り込む
func NewConfig(name string, args []string) (*Config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage: gorviz-launcher [-addr] [-agent-addr] [-save-dir] [agent flags] [-- command]

start a gorviz server and a gorviz agent together (local use). With "-- command"
it also starts the target and records its stdout and stderr as logs:

  gorviz-launcher -target http://localhost:6060 -- go run ./myapp

`)
		fs.PrintDefaults()
	}
	addr := fs.String("addr", "localhost:7070", "listen address of the GUI and the API")
	agentAddr := fs.String("agent-addr", "localhost:7071", "TCP address between the server and the agent")
	saveDir := fs.String("save-dir", "", "directory where the server saves recordings around issues")
	if len(args) > 0 && (args[0] == "-h" || args[0] == "-help" || args[0] == "--help") {
		fs.Usage()
		return nil, flag.ErrHelp
	}
	var command []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, command = args[:i], args[i+1:]
	}
	// agent のフラグはそのまま渡すので、ここでは知らないフラグで止まらないよう、自分のフラグだけを先頭から読む
	known, rest := splitKnown(args, map[string]bool{"addr": true, "agent-addr": true, "save-dir": true})
	if err := fs.Parse(known); err != nil {
		return nil, err
	}
	server, err := serverconfig.NewConfig(name, []string{"-addr", *addr, "-agent-addr", *agentAddr, "-save-dir", *saveDir})
	if err != nil {
		return nil, err
	}
	agent, err := agentconfig.NewConfig(name, append([]string{"-agent-addr", *agentAddr}, rest...))
	if err != nil {
		return nil, err
	}
	if len(command) > 0 {
		agent.LogsFrom = "-"
	}
	return &Config{Server: server, Agent: agent, TargetCommand: command}, nil
}

// splitKnown は先頭から自分の知っているフラグ (-name value / -name=value) だけを抜き出し、残りを順番どおり返す
func splitKnown(args []string, names map[string]bool) (known, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := a
		for len(name) > 0 && name[0] == '-' {
			name = name[1:]
		}
		key, _, hasValue := strings.Cut(name, "=")
		if !names[key] {
			rest = append(rest, a)
			continue
		}
		known = append(known, a)
		if !hasValue && i+1 < len(args) {
			known = append(known, args[i+1])
			i++
		}
	}
	return known, rest
}
