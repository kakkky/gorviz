#!/bin/sh
# air は Interrupt をこのシェルにしか送らないので、todo-app と launcher に配ってから、両方が終わるのを待つ。
# 届かないと古い launcher が 7070 を持ったまま残り、作り直した launcher が起動できない
set -eu
fifo=$(mktemp -u)
mkfifo "$fifo"
TODO_ADDR=0.0.0.0:8090 ./bin/air/todo-app >"$fifo" 2>&1 &
app=$!
./bin/air/gorviz-launcher --pprof-addr localhost:6060 --service todo --http-addr 0.0.0.0:7070 --data-dir bin/data <"$fifo" &
launcher=$!
trap 'kill -INT "$app" "$launcher" 2>/dev/null || true' INT TERM
wait "$launcher" || true
wait "$app" || true
rm -f "$fifo"
