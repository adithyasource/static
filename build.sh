#!/bin/sh
set -e

mkdir -p dist
go build -trimpath -ldflags "-s -w" -o dist/static .
