// Package config は gorviz-launcher の flag
package config

import (
	"flag"
	"fmt"
	"strings"

	agentconfig "github.com/kakkky/gorviz/internal/app/agent/config"
	serverconfig "github.com/kakkky/gorviz/internal/app/server/config"
)

type Config struct {
	Server *serverconfig.Config
	Agent  *agentconfig.Config
}

// NewConfig は -addr・-agent-addr・-save-dir を server に、残りのフラグを agent に渡す。-agent-addr は両方に渡す
func NewConfig(name string, args []string) (*Config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage: gorviz-launcher [-addr] [-agent-addr] [-save-dir] [agent flags]

start a gorviz server and a gorviz agent together (local use). With "-logs -"
the agent reads the target's logs piped to stdin

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
	return &Config{Server: server, Agent: agent}, nil
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
