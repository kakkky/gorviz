// Package response は Accept に合わせて、HTTP API の応答を TOON か JSON で書き出す
package response

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kakkky/gorviz/internal/pkg/toon"
	"github.com/kakkky/gorviz/internal/protocol/serverapi"
)

// Write は Accept が JSON なら JSON、それ以外 (TOON を含む) なら TOON で書く
func Write(rw http.ResponseWriter, r *http.Request, status int, v any) error {
	var (
		body []byte
		err  error
		ct   string
	)
	if strings.Contains(r.Header.Get("Accept"), serverapi.MediaTypeJSON) {
		body, err = json.MarshalIndent(v, "", "  ")
		ct = serverapi.MediaTypeJSON
	} else {
		body, err = toon.Marshal(v)
		ct = serverapi.MediaTypeTOON + "; charset=utf-8"
	}
	if err != nil {
		return err
	}
	rw.Header().Set("Content-Type", ct)
	rw.WriteHeader(status)
	_, err = rw.Write(body)
	return err
}
