TEMPL_VERSION := v0.3.1070
BUF_VERSION := v1.75.0

.PHONY: build test templ.gen proto.gen mock.gen gen launcher.up example.todo

# gorviz (CLI)・gorviz-launcher・gorviz-agent・gorviz-server・gorviz-mcp を bin/ に作る
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

# agent と server の契約 (internal/protocol/agentrpc/agent.proto) から gRPC のコードを作る。プラグインの版は buf.gen.yaml で固定している
proto.gen:
	go run github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION) generate

mock.gen:
	go generate ./...

gen: templ.gen proto.gen mock.gen

# 観測する Todo アプリ (example/todo-app, pprof は localhost:6060) の出力をパイプで launcher に渡す。GUI は http://localhost:7070
launcher.up: build
	go run ./example/todo-app 2>&1 | ./bin/gorviz-launcher --pprof-addr localhost:6060 --service todo --data-dir bin/data

example.todo:
	go run ./example/todo-app
