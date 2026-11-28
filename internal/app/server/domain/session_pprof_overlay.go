package domain

import "path/filepath"

// PprofSocketEnv は、server がアプリに渡す、pprof を開く unix socket の環境変数
const PprofSocketEnv = "GORVIZ_PPROF_SOCKET"

// pprofServeFunction は差し込む関数の名前。作られたとき・最初に見えたときのスタックにこれがある goroutine は gorviz のもの
const pprofServeFunction = "gorvizServePprof"

// MainPackage は pprof を開く処理を差し込むアプリの main パッケージ
type MainPackage struct {
	ImportPath string
	Dir        string
}

// Overlay は go build -overlay に渡す、置き換える (足す) ファイルの絶対パスと中身
type Overlay map[string][]byte

// PprofOverlay は、アプリの main パッケージに、pprof を unix socket で開くファイルを足す。
// アプリ本体の待ち受けとは別に開くので、アプリが自分の HTTP サーバーを閉じても、プロセスが終わるまで取れる
func PprofOverlay(main MainPackage) Overlay {
	return Overlay{filepath.Join(main.Dir, "gorviz_pprof.go"): []byte(pprofSource)}
}

const pprofSource = `package main

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
)

func init() { ` + pprofServeFunction + `() }

func ` + pprofServeFunction + `() {
	ln, err := net.Listen("unix", os.Getenv("` + PprofSocketEnv + `"))
	if err != nil {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	go http.Serve(ln, mux)
}
`
