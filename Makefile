GO ?= go
GOCACHE ?= /tmp/foxlab-cli-go-cache
GOPROXY ?= off

.PHONY: build test start dev smoke

build:
	mkdir -p bin
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) build -buildvcs=false -o bin/foxlab-cli .

test:
	GOCACHE="$(GOCACHE)" $(GO) test ./...

start: dev

dev:
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) run . $(RUN_ARGS)

smoke:
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) run . --no-raw --width 90 --height 24 $(RUN_ARGS)
