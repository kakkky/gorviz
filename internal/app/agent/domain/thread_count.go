package domain

import (
	"bytes"
	"strconv"
)

// ParseThreadCount は threadcreate プロファイル (debug=1) の見出しから、runtime が持っている OS のスレッドの数を読む
func ParseThreadCount(profile []byte) (int, error) {
	line, _, _ := bytes.Cut(profile, []byte("\n"))
	rest, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("threadcreate profile: total "))
	if !ok {
		return 0, ErrInvalidArgument.With("not a threadcreate profile")
	}
	n, err := strconv.Atoi(string(rest))
	if err != nil {
		return 0, ErrInvalidArgument.Wrap(err, "parse thread count")
	}
	return n, nil
}
