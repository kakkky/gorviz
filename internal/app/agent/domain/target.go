package domain

import (
	"cmp"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

type Mode string

const (
	ModeSidecar Mode = "sidecar"
	ModeDaemon  Mode = "daemon"
	ModeLibrary Mode = "library"
)

func NewMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeSidecar, ModeDaemon, ModeLibrary:
		return m, nil
	}
	return "", ErrInvalidArgument.With("unknown mode " + strconv.Quote(s))
}

// TargetID は観測するアプリを、インスタンスの入れ替わりをまたいで指す。sidecar 型・ライブラリ型は 1 つ、daemon 型は Pod の UID
type TargetID string

const selfTargetID TargetID = "self"

// TargetInstance は観測するアプリ 1 つと、今のインスタンス。LogDir はそのアプリのログファイルの置き場で、空なら取り込んだログをすべてこのアプリのものとする
type TargetInstance struct {
	ID        TargetID
	Instance  observation.Instance
	PprofAddr string
	LogDir    string
}

// firstSidecarIncarnation は sidecar 型で、agent が見始めたときのプロセス。agent だけが再起動しても同じインスタンスのままにするため、決まった値にする
const firstSidecarIncarnation observation.Incarnation = "1"

// SelfTarget は sidecar 型・ライブラリ型の観測するアプリ。ライブラリ型は agent がアプリと一緒に起動し直すので、agent の起動ごとの ID をそのままプロセスの見分けに使う
func SelfTarget(mode Mode, service observation.ServiceName, name observation.InstanceName, pprofAddr string, agent observation.AgentID) TargetInstance {
	inc := firstSidecarIncarnation
	if mode == ModeLibrary {
		inc = observation.Incarnation(agent)
	}
	return TargetInstance{ID: selfTargetID, Instance: observation.Instance{Service: service, Name: name, Incarnation: inc}, PprofAddr: pprofAddr}
}

const (
	AnnotationPprofPort    = "gorviz/pprof-port"
	AnnotationLogContainer = "gorviz/log-container"
)

// Pod は Kubernetes API から読んだ Pod のうち、観測に使うところ
type Pod struct {
	UID          string
	Namespace    string
	Name         string
	IP           string
	Running      bool
	Labels       map[string]string
	Annotations  map[string]string
	Containers   []string
	ContainerIDs map[string]string
}

// TargetsFromPods は annotation の付いた、動いている Pod を観測するアプリにする。
// コンテナの ID が変わればプロセスが入れ替わったので、別のインスタンスになる
func TargetsFromPods(pods []Pod, podLogDir string) []TargetInstance {
	var out []TargetInstance
	for _, p := range pods {
		port, ok := p.Annotations[AnnotationPprofPort]
		if !ok || !p.Running || p.IP == "" {
			continue
		}
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			continue
		}
		container := p.Annotations[AnnotationLogContainer]
		if container == "" && len(p.Containers) > 0 {
			container = p.Containers[0]
		}
		out = append(out, TargetInstance{
			ID: TargetID(p.UID),
			Instance: observation.Instance{
				Service:     podService(p, container),
				Name:        observation.InstanceName(p.Namespace + "/" + p.Name),
				Incarnation: observation.Incarnation(p.ContainerIDs[container]),
			},
			PprofAddr: p.IP + ":" + port,
			LogDir:    filepath.Join(podLogDir, p.Namespace+"_"+p.Name+"_"+p.UID, container),
		})
	}
	slices.SortFunc(out, func(a, b TargetInstance) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// podService は Pod が入れ替わっても変わらない名前を、慣習のラベルから取る。なければログを取るコンテナの名前
func podService(p Pod, container string) observation.ServiceName {
	for _, k := range []string{"app.kubernetes.io/name", "app"} {
		if v := p.Labels[k]; v != "" {
			return observation.ServiceName(v)
		}
	}
	return observation.ServiceName(container)
}

// SyncTargets は見つけたアプリで一覧を置き換える。sidecar 型は agent がプロセスの入れ替わりを見分けるので、今のインスタンスを引き継ぐ。
// ended は一覧から外れたか、プロセスが入れ替わったインスタンスで、送り切ってから外す
func SyncTargets(current, found []TargetInstance, mode Mode) (next []TargetInstance, ended []observation.Instance) {
	byID := make(map[TargetID]TargetInstance, len(current))
	for _, t := range current {
		byID[t.ID] = t
	}
	seen := make(map[TargetID]bool, len(found))
	for _, t := range found {
		seen[t.ID] = true
		if prev, ok := byID[t.ID]; ok {
			if mode == ModeSidecar {
				t.Instance.Incarnation = prev.Instance.Incarnation
			}
			if prev.Instance != t.Instance {
				ended = append(ended, prev.Instance)
			}
		}
		next = append(next, t)
	}
	for _, t := range current {
		if !seen[t.ID] {
			ended = append(ended, t.Instance)
		}
	}
	return next, ended
}

// ProcessReplaced は trace から、同じ名前のまま中のプロセスが入れ替わったと分かるか。sidecar 型だけが trace で見分ける。
// 比べるのは、前のチャンクの終わりに生きていた goroutine の ID の最大。これまでに見た ID の最大と比べると、ID の大きい goroutine が終わっただけで減ってしまう。
// ID はプロセスの中で使い回されないので、同じプロセスならチャンクの最大がそれを下回らない
func ProcessReplaced(mode Mode, prevMaxAlive, curMax observation.GoroutineID) bool {
	return mode == ModeSidecar && prevMaxAlive > 0 && curMax > 0 && curMax < prevMaxAlive
}

// MaxAliveGoroutineID はチャンクの終わりに生きている goroutine の ID の最大
func MaxAliveGoroutineID(parsed ParsedTrace) observation.GoroutineID {
	var out observation.GoroutineID
	for id, g := range parsed.Goroutines {
		if g.Alive {
			out = max(out, id)
		}
	}
	return out
}

func NewIncarnation(at time.Time) observation.Incarnation {
	return observation.Incarnation(strconv.FormatInt(at.UnixNano(), 10))
}

// ReplaceInstance は id のアプリのインスタンスを置き換える
func ReplaceInstance(targets []TargetInstance, id TargetID, in observation.Instance) []TargetInstance {
	out := slices.Clone(targets)
	for i := range out {
		if out[i].ID == id {
			out[i].Instance = in
		}
	}
	return out
}

// ResolveLogTarget は取り込んだ 1 行がどのアプリのものかを決める。ファイルはアプリのログの置き場の下にあるかで、ほかは観測するアプリが 1 つのときだけ決まる
func ResolveLogTarget(targets []TargetInstance, source observation.LogSource, path string) (TargetInstance, bool) {
	if source == observation.LogSourceFile {
		for _, t := range targets {
			if t.LogDir != "" && strings.HasPrefix(filepath.Clean(path), filepath.Clean(t.LogDir)+string(filepath.Separator)) {
				return t, true
			}
		}
	}
	var only []TargetInstance
	for _, t := range targets {
		if t.LogDir == "" {
			only = append(only, t)
		}
	}
	if len(only) == 1 {
		return only[0], true
	}
	return TargetInstance{}, false
}

// TargetState は観測するアプリごとに、前のチャンクから持ち越す状態。Instance が変わったら持ち越さない
type TargetState struct {
	Instance            observation.Instance
	MaxAliveGoroutineID observation.GoroutineID
	LastTraceEnd        time.Time
	LastLeakCheck       time.Time
}

// For は今のインスタンスの分だけを返す。別のインスタンスのものは、新しいプロセスには引き継がない
func (s TargetState) For(in observation.Instance) TargetState {
	if s.Instance != in {
		return TargetState{Instance: in}
	}
	return s
}

func TargetIDs(targets []TargetInstance) []TargetID {
	ids := make([]TargetID, len(targets))
	for i, t := range targets {
		ids[i] = t.ID
	}
	return ids
}
