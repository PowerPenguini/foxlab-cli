GO ?= go
GOCACHE ?= /tmp/foxlab-cli-go-cache
GOPROXY ?= off
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
INSTALL ?= install

.PHONY: build install test start dev smoke

build:
	mkdir -p bin
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) build -buildvcs=false -o bin/foxlab-cli .

install: build
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL) -m 0755 bin/foxlab-cli "$(DESTDIR)$(BINDIR)/foxlab-cli"

test:
	GOCACHE="$(GOCACHE)" $(GO) test ./...

start: dev

dev:
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) run . $(RUN_ARGS)

smoke:
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) run . --no-raw --width 90 --height 24 $(RUN_ARGS)
