// Package agent は agent との決まり。agent と server の間の 1 本の接続に流すメッセージの形と区切り。
// agent と一緒にアプリのビルドに持ち込まれるので、標準ライブラリだけで書く
package agent

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// EnvSocket は、server がアプリに渡す、agent がつなぐ先の環境変数
const EnvSocket = "GORVIZ_AGENT_SOCKET"

// TraceCategory は、agent が execution trace に書く出来事のカテゴリ
const TraceCategory = "gorviz"

// Kind はメッセージの種類。消した種類の番号は使い回さない
type Kind uint8

const (
	KindHello                Kind = 1
	KindTrace                Kind = 2
	KindReply                Kind = 3
	KindGoroutineLeakProfile Kind = 4
	KindCoverage             Kind = 5
	KindBye                  Kind = 6

	KindInstruction Kind = 16
)

const maxPayload = 1 << 30

type Message struct {
	Kind    Kind
	Payload []byte
}

// Hello は、つないだ直後に agent が送る
type Hello struct {
	GoVersion string `json:"goVersion"`
	Procs     int    `json:"procs"`
	PID       int    `json:"pid"`
}

// Op は server から agent への指示の種類
type Op string

const (
	OpWriteTraceEvent    Op = "write-trace-event"
	OpResetCoverage      Op = "reset-coverage"
	OpWriteCoverage      Op = "write-coverage"
	OpWriteGoroutineLeak Op = "write-goroutine-leak"
	OpWriteThreadCount   Op = "write-thread-count"
	OpFinish             Op = "finish"
)

// Instruction の Event は OpWriteTraceEvent で trace に書く出来事
type Instruction struct {
	ID    uint64 `json:"id"`
	Op    Op     `json:"op"`
	Event string `json:"event,omitempty"`
}

// Reply は指示 1 つへの返事。Coverage は OpWriteCoverage のときだけ載る
type Reply struct {
	ID       uint64         `json:"id"`
	Error    string         `json:"error,omitempty"`
	Coverage []CoverageFile `json:"coverage,omitempty"`
}

// CoverageFile は runtime/coverage が書き出したファイル 1 つ (covmeta.* か covcounters.*)
type CoverageFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// GoroutineLeakProfile の Seq は、書き出す直前に trace に書いた出来事 (LeakCheckEvent) と同じ番号。プロファイルを取った時刻を trace の上で知るため
type GoroutineLeakProfile struct {
	Seq  uint64 `json:"seq"`
	Text []byte `json:"text"`
}

type Coverage struct {
	Files []CoverageFile `json:"files"`
}

// 出来事の名前。trace の TraceCategory のログとして書く。EventAppExit はアプリが終わって agent が最後の観測データを送る前に書く
const (
	EventAppExit           = "app exit"
	EventLeakCheckPrefix   = "leak-check "
	EventThreadCountPrefix = "threads "
)

func LeakCheckEvent(seq uint64) string {
	return fmt.Sprintf("%s%d", EventLeakCheckPrefix, seq)
}

func ThreadCountEvent(n int) string {
	return fmt.Sprintf("%s%d", EventThreadCountPrefix, n)
}

// Write は 1 バイトの種類・4 バイトの長さ・中身の順で書く
func Write(w io.Writer, m Message) error {
	if len(m.Payload) > maxPayload {
		return fmt.Errorf("agent protocol: payload of %d bytes is too large", len(m.Payload))
	}
	var head [5]byte
	head[0] = byte(m.Kind)
	binary.BigEndian.PutUint32(head[1:], uint32(len(m.Payload)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err := w.Write(m.Payload)
	return err
}

func Read(r *bufio.Reader) (Message, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return Message{}, err
	}
	n := binary.BigEndian.Uint32(head[1:])
	if n > maxPayload {
		return Message{}, fmt.Errorf("agent protocol: payload of %d bytes is too large", n)
	}
	m := Message{Kind: Kind(head[0]), Payload: make([]byte, n)}
	if _, err := io.ReadFull(r, m.Payload); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Message{}, err
	}
	return m, nil
}

// Encode は JSON の中身を持つ種類のメッセージを作る
func Encode(kind Kind, v any) (Message, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return Message{}, err
	}
	return Message{Kind: kind, Payload: b}, nil
}

func Decode[T any](m Message) (T, error) {
	var v T
	err := json.Unmarshal(m.Payload, &v)
	return v, err
}
