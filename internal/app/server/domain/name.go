package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/kakkky/gorviz/internal/pkg/funcname"
)

const (
	readableMax = 48
	hashLen     = 6
)

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// FileName は文字列から <読める形>--...--<ハッシュ> を作る。ハッシュは元の文字列すべてから作るので、切り詰めても重ならない
func FileName(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	out := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		out = append(out, readable(p))
	}
	return strings.Join(append(out, hex.EncodeToString(h[:])[:hashLen]), "--")
}

func readable(s string) string {
	r := strings.Trim(unsafeChars.ReplaceAllString(s, "-"), "-")
	if len(r) > readableMax {
		r = strings.TrimRight(r[:readableMax], "-")
	}
	if r == "" {
		return "-"
	}
	return r
}

var (
	shapeQuoted = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	shapeHex    = regexp.MustCompile(`\b(0x[0-9a-fA-F]+|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]*[0-9][0-9a-fA-F]*[a-fA-F][0-9a-fA-F]*)\b`)
	shapeNumber = regexp.MustCompile(`\d+(\.\d+)?`)
	shapeSpace  = regexp.MustCompile(`\s+`)
)

// MessageShape はメッセージの値の部分 (数・ID・引用した文字列) をならし、同じ形のメッセージを 1 つにまとめる
func MessageShape(msg string) string {
	s := shapeQuoted.ReplaceAllString(msg, "s")
	s = shapeHex.ReplaceAllString(s, "n")
	s = shapeNumber.ReplaceAllString(s, "n")
	return strings.ToLower(strings.TrimSpace(shapeSpace.ReplaceAllString(s, " ")))
}

// NormalizeFunction は数える・比べる単位の関数にする。無名関数は外側に、型パラメータはならす
func NormalizeFunction(fn string) string {
	return funcname.FoldTypeParams(funcname.Outer(fn))
}
