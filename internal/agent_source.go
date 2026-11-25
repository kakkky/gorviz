// Package internal は、server がアプリのビルドに差し込む agent と agentsocket のソースを、ファイルとして埋め込む
package internal

import (
	"embed"
	"io/fs"
)

//go:embed app/agent protocol/agentsocket
var agentSource embed.FS

// AgentSource は agent の根 (app/agent) と agentsocket の根 (protocol/agentsocket) を返す
func AgentSource() (agent fs.FS, socket fs.FS, err error) {
	agent, err = fs.Sub(agentSource, "app/agent")
	if err != nil {
		return nil, nil, err
	}
	socket, err = fs.Sub(agentSource, "protocol/agentsocket")
	if err != nil {
		return nil, nil, err
	}
	return agent, socket, nil
}
