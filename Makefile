TEMPL_VERSION := v0.3.1070

.PHONY: build test vet gen templ.gen mock.gen ts.gen assets.build assets.test example example.stop

# gorviz (server と cli を 1 つに持つ実行ファイル) を bin/ に作る
build:
	go build -o bin/gorviz ./cmd/gorviz

test:
	go test -count=1 ./...

vet:
	go vet ./...

templ.gen:
	go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION) generate

# domain の interface から mock を、GUI に渡す Go の型から TS の型を作る
mock.gen ts.gen:
	go generate ./...

gen: templ.gen mock.gen

# GUI の TS と CSS を internal/app/server/assets/dist に書き出す。npm を使うのは開発のときだけ
assets.build:
	cd internal/app/server/assets && npm ci && npm run typecheck && npm run build

assets.test:
	cd internal/app/server/assets && npm test

# 例のアプリ (example/todo-app) を gorviz で動かし、シナリオで診断する
example: build
	./bin/gorviz run ./example/todo-app
	./bin/gorviz diagnose example/todo-app/scenario.yaml || true

example.stop:
	./bin/gorviz stop
