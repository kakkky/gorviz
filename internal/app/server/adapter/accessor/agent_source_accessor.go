package accessor

import (
	"io/fs"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// AgentSourceAccessor は server に埋め込んだ agent のソースを読む
type AgentSourceAccessor struct {
	agent  fs.FS
	socket fs.FS
}

func NewAgentSourceAccessor(agent, socket fs.FS) *AgentSourceAccessor {
	return &AgentSourceAccessor{agent: agent, socket: socket}
}

func (a *AgentSourceAccessor) AgentFiles() ([]domain.SourceFile, []domain.SourceFile, error) {
	agent, err := readAll(a.agent)
	if err != nil {
		return nil, nil, err
	}
	socket, err := readAll(a.socket)
	if err != nil {
		return nil, nil, err
	}
	return agent, socket, nil
}

func readAll(root fs.FS) ([]domain.SourceFile, error) {
	var out []domain.SourceFile
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		out = append(out, domain.SourceFile{Path: p, Data: b})
		return nil
	})
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "read agent source")
	}
	return out, nil
}
