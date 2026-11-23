// Package authtoken は発行したトークンを持ち、agent と HTTP API・GUI のトークンを確かめる
package authtoken

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
)

const file = "server/auth-tokens.jsonl"

type Kind string

const (
	KindAgent Kind = "agent"
	KindAPI   Kind = "api"
)

// record はトークン 1 つ。トークンそのものは持たず、SHA-256 だけを持つ
type record struct {
	ID      string     `json:"id"`
	Kind    Kind       `json:"kind"`
	SHA256  string     `json:"sha256"`
	Created time.Time  `json:"created"`
	Revoked *time.Time `json:"revoked,omitempty"`
}

// Tokens は auth-tokens.jsonl を読んで持つ。ファイルが書き換えられたら読み直すので、行を消す (または revoked を付ける) と、そのトークンは使えなくなる
type Tokens struct {
	root    *filesystem.Root
	mu      sync.Mutex
	records []record
	modTime time.Time
}

// NewTokens は bootstrap を agent と API の両方のトークンとして持たせる。トークンが 1 つもなければ 1 つずつ発行してログに 1 度だけ出す
func NewTokens(root *filesystem.Root, bootstrap string) (*Tokens, error) {
	t := &Tokens{root: root}
	if err := t.load(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	changed := false
	if bootstrap != "" {
		for _, k := range []Kind{KindAgent, KindAPI} {
			if !t.valid(k, bootstrap) {
				t.records = append(t.records, newRecord(k, bootstrap, now))
				changed = true
			}
		}
	}
	if len(t.records) == 0 {
		for _, k := range []Kind{KindAgent, KindAPI} {
			tok, err := randomToken()
			if err != nil {
				return nil, err
			}
			t.records = append(t.records, newRecord(k, tok, now))
			slog.Info("issued a token; it is shown only once", slog.String("kind", string(k)), slog.String("token", tok))
		}
		changed = true
	}
	if changed {
		if err := t.save(); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func newRecord(k Kind, token string, now time.Time) record {
	sum := sha256.Sum256([]byte(token))
	return record{ID: string(k) + "-" + hex.EncodeToString(sum[:4]), Kind: k, SHA256: hex.EncodeToString(sum[:]), Created: now}
}

func randomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "gzv_" + hex.EncodeToString(b[:]), nil
}

func (t *Tokens) load() error {
	st, err := t.root.Stat(file)
	if errors.Is(err, fs.ErrNotExist) {
		t.records, t.modTime = nil, time.Time{}
		return nil
	}
	if err != nil {
		return err
	}
	b, err := t.root.ReadFile(file)
	if err != nil {
		return err
	}
	var records []record
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var r record
		if len(line) < 10 || line[8] != ' ' {
			slog.Warn("skip a broken token line")
			continue
		}
		sum, err := strconv.ParseUint(string(line[:8]), 16, 32)
		if err != nil || uint32(sum) != crc32.ChecksumIEEE(line[9:]) || json.Unmarshal(line[9:], &r) != nil {
			slog.Warn("skip a token line whose checksum does not match")
			continue
		}
		records = append(records, r)
	}
	t.records, t.modTime = records, st.ModTime()
	return nil
}

func (t *Tokens) save() error {
	var buf []byte
	for _, r := range t.records {
		js, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf = fmt.Appendf(buf, "%08x %s\n", crc32.ChecksumIEEE(js), js)
	}
	if err := t.root.Replace(file, buf); err != nil {
		return err
	}
	st, err := t.root.Stat(file)
	if err != nil {
		return err
	}
	t.modTime = st.ModTime()
	return nil
}

func (t *Tokens) valid(k Kind, token string) bool {
	if token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	want := hex.EncodeToString(sum[:])
	for _, r := range t.records {
		if r.Kind == k && r.Revoked == nil && subtle.ConstantTimeCompare([]byte(r.SHA256), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

func (t *Tokens) check(k Kind, token string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st, err := t.root.Stat(file); err == nil && !st.ModTime().Equal(t.modTime) {
		if err := t.load(); err != nil {
			slog.Warn("reload tokens", slog.Any("err", err))
		}
	}
	return t.valid(k, token)
}

func (t *Tokens) ValidAgentToken(token string) bool { return t.check(KindAgent, token) }

func (t *Tokens) ValidAPIToken(token string) bool { return t.check(KindAPI, token) }
