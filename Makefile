TEMPL_VERSION := v0.3.1070

.PHONY: build test templ.gen wire.gen gen example.leaky top serve

build:
	go build -o bin/gorviz ./cmd/gorviz

test:
	go test -count=1 ./...

templ.gen:
	go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION) generate

wire.gen:
	go run github.com/google/wire/cmd/wire@latest gen ./cmd/gorviz

gen: templ.gen wire.gen

# 動作確認用の観測対象。localhost:6060 で pprof を公開し、goroutine をリークし続ける
example.leaky:
	go run ./example/leaky

top:
	go run ./cmd/gorviz top

serve:
	go run ./cmd/gorviz serve
