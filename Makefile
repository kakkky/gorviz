TEMPL_VERSION := v0.3.1070

.PHONY: build test templ.gen gen example.leaky example.library overhead up

# gorviz (CLI)・gorviz-agent・gorviz-server・gorviz-tui・gorviz-mcp を bin/ に作る。gorviz up は同じディレクトリの agent と server を起動する
build:
	go build -o bin/gorviz ./cmd/cli
	go build -o bin/gorviz-agent ./cmd/agent
	go build -o bin/gorviz-server ./cmd/server
	go build -o bin/gorviz-tui ./cmd/tui
	go build -o bin/gorviz-mcp ./cmd/mcp

test:
	go test -count=1 ./...

templ.gen:
	go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION) generate

gen: templ.gen

# 動作確認用の観測対象を gorviz の下で起動する。リークし続け、ときどきロック待ちで遅いリクエストを出す
up: build
	go build -o bin/leaky ./example/leaky
	./bin/gorviz up -save-dir bin/recordings -dump-interval 5s -leak-check-every 2 -- ./bin/leaky

# 動作確認用の観測対象。localhost:6060 で pprof を公開する (agent を別に起動して観測する)
example.leaky:
	go run ./example/leaky

# ライブラリ型の動作確認用。gorviz-server (agent の口 localhost:7071) にアプリの中から直接つなぐ
example.library:
	go run ./example/library

# gorviz が対象にかける負荷の目安を測る
overhead:
	go run ./example/overhead
