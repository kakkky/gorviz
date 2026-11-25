TEMPL_VERSION := v0.3.1070
APP ?= ./example/todo-app

.PHONY: build run stop vet gen test

# gorviz (server と cli を 1 つに持つ実行ファイル) を bin/ に作る
build:
	go build -o bin/gorviz ./cmd/gorviz

# make run APP=<main package> で、そのアプリを gorviz の下で動かす
run: build
	./bin/gorviz run $(APP)

stop:
	./bin/gorviz stop

vet:
	go vet ./...

# templ、domain の interface からの mock、GUI に渡す Go の型からの TS の型、GUI の TS と CSS (assets/dist) を作る。npm を使うのは開発のときだけ
gen:
	go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION) generate
	go generate ./...
	cd internal/app/server/assets && npm ci && npm run typecheck && npm run build

test:
	go test -count=1 ./...
	cd internal/app/server/assets && npm test
