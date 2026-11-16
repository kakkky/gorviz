package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 時刻の入力欄 (<input type="datetime-local">) の値の形。秒は省かれることがある
var localTimeLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04"}

const localTimeInputLayout = "2006-01-02T15:04:05"

// localTimeParam は時刻の入力欄の値を、server のタイムゾーンの時刻として読む。画面の時刻もすべて server のタイムゾーンで出している
func localTimeParam(r *http.Request, key string) (time.Time, bool, error) {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range localTimeLayouts {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, true, nil
		}
	}
	return time.Time{}, false, errors.New("cannot read the time " + strconv.Quote(v))
}

// timeZoneLabel は入力欄の横に出す、server のタイムゾーンの名前
func timeZoneLabel() string {
	name, _ := time.Now().Zone()
	return name
}
