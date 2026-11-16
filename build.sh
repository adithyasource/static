#!/bin/sh
set -e

mkdir -p dist

case "$1" in
  --linux)
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/static-linux-amd64 .
    ;;
  *)
    go build -trimpath -ldflags "-s -w" -o dist/static .
    ;;
esac
