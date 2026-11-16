TEMPL_VERSION := v0.3.1070

.PHONY: build test templ.gen gen example.todo example.library overhead launcher.up

# gorviz (CLI)・gorviz-launcher・gorviz-agent・gorviz-server・gorviz-mcp を bin/ に作る。gorviz-launcher は server と agent を 1 つのプロセスで動かす
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

# ローカルで server と agent と観測対象の Todo アプリ (example/todo) をまとめて起動する。対象のログも取り込む。
# Todo アプリは http://localhost:8080、gorviz は http://localhost:7070
launcher.up: build
	./bin/gorviz-launcher -target http://localhost:6060 -save-dir bin/recordings -dump-interval 5s -leak-check-every 2 -- go run ./example/todo

# 動作確認用の観測対象の Todo アプリ。http://localhost:8080 で開き、localhost:6060 で pprof を公開する (agent を別に起動して観測する)
example.todo:
	go run ./example/todo

# ライブラリ型の動作確認用。gorviz-server (agent の口 localhost:7071) にアプリの中から直接つなぐ
example.library:
	go run ./example/library

# gorviz が対象にかける負荷の目安を測る
overhead:
	go run ./example/overhead
