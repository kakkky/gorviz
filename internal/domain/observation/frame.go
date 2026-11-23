package observation

import "strconv"

type Frame struct {
	Function string
	File     string
	Line     int
}

func (f Frame) Location() string {
	if f.File == "" {
		return ""
	}
	return f.File + ":" + strconv.Itoa(f.Line)
}
