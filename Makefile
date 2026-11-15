TEMPL_VERSION := v0.3.1070

.PHONY: build test templ.gen gen example.leaky example.library overhead launcher.up

# gorviz (CLI)・gorviz-launcher・gorviz-agent・gorviz-server・gorviz-mcp を bin/ に作る。gorviz-launcher は同じディレクトリの agent と server を起動する
build:
	go build -o bin/gorviz ./cmd/cli
	go build -o bin/gorviz-launcher ./cmd/launcher
	go build -o bin/gorviz-agent ./cmd/agent
	go build -o bin/gorviz-server ./cmd/server
	go build -o bin/gorviz-mcp ./cmd/mcp

test:
	go test -count=1 ./...

templ.gen:
	go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION) generate

gen: templ.gen

# ローカルで server と agent をまとめて起動する。対象 (localhost:6060) は example.leaky などで別に起動しておく
launcher.up: build
	./bin/gorviz-launcher -target http://localhost:6060 -save-dir bin/recordings -dump-interval 5s -leak-check-every 2

# 動作確認用の観測対象。localhost:6060 で pprof を公開する (agent を別に起動して観測する)
example.leaky:
	go run ./example/leaky

# ライブラリ型の動作確認用。gorviz-server (agent の口 localhost:7071) にアプリの中から直接つなぐ
example.library:
	go run ./example/library

# gorviz が対象にかける負荷の目安を測る
overhead:
	go run ./example/overhead
