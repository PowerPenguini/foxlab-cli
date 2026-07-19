GO ?= go
GOCACHE ?= /tmp/foxlab-cli-go-cache
GOPROXY ?= https://proxy.golang.org,direct
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
SYSTEMD_SYSTEM_UNIT_DIR ?= $(PREFIX)/lib/systemd/system
INSTALL ?= install
SED ?= sed
SUDO ?= sudo
ROOT_PREFIX ?= $(shell if [ -z "$(DESTDIR)" ] && [ "$$(id -u)" != "0" ]; then printf '$(SUDO)'; fi)
INSTALL_CMD ?= $(ROOT_PREFIX) $(INSTALL)
RM_CMD ?= $(ROOT_PREFIX) rm
SYSTEMCTL_USER ?= $(shell if [ "$$(id -u)" = "0" ] && [ -n "$$SUDO_USER" ] && [ "$$SUDO_USER" != "root" ]; then uid="$$(id -u "$$SUDO_USER" 2>/dev/null)"; if [ -n "$$uid" ] && command -v runuser >/dev/null 2>&1; then printf 'runuser -u %s -- env XDG_RUNTIME_DIR=/run/user/%s systemctl --user' "$$SUDO_USER" "$$uid"; else printf 'systemctl --user'; fi; else printf 'systemctl --user'; fi)
SYSTEMCTL ?= $(ROOT_PREFIX) systemctl

.PHONY: build install uninstall test start dev smoke

build:
	mkdir -p bin
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) build -buildvcs=false -o bin/foxlab ./cmd/foxlab
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) build -buildvcs=false -o bin/foxlabd ./cmd/foxlabd

install: build
	$(INSTALL_CMD) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL_CMD) -m 0755 bin/foxlab "$(DESTDIR)$(BINDIR)/foxlab"
	$(INSTALL_CMD) -m 0755 bin/foxlabd "$(DESTDIR)$(BINDIR)/foxlabd"
	$(INSTALL_CMD) -d "$(DESTDIR)$(SYSTEMD_SYSTEM_UNIT_DIR)"
	@set -eu; \
	tmp="$$(mktemp)"; \
	trap 'rm -f "$$tmp"' EXIT; \
	$(SED) "s|@BINDIR@|$(BINDIR)|g" packaging/systemd/system/foxlabd.service.in > "$$tmp"; \
	$(INSTALL_CMD) -m 0644 "$$tmp" "$(DESTDIR)$(SYSTEMD_SYSTEM_UNIT_DIR)/foxlabd.service"
	@if [ -z "$(DESTDIR)" ]; then \
		$(SYSTEMCTL_USER) disable --now foxlabd.service 2>/dev/null || true; \
		$(SYSTEMCTL) daemon-reload; \
		$(SYSTEMCTL) enable foxlabd.service; \
		$(SYSTEMCTL) restart foxlabd.service; \
	fi

uninstall:
	@if [ -z "$(DESTDIR)" ]; then \
		$(SYSTEMCTL) disable --now foxlabd.service 2>/dev/null || true; \
		$(SYSTEMCTL_USER) disable --now foxlabd.service 2>/dev/null || true; \
	fi
	$(RM_CMD) -f "$(DESTDIR)$(BINDIR)/foxlab" "$(DESTDIR)$(BINDIR)/foxlabd"
	$(RM_CMD) -f "$(DESTDIR)$(SYSTEMD_SYSTEM_UNIT_DIR)/foxlabd.service"
	@if [ -z "$(DESTDIR)" ]; then \
		$(SYSTEMCTL) daemon-reload 2>/dev/null || true; \
		$(SYSTEMCTL_USER) daemon-reload 2>/dev/null || true; \
	fi

test:
	GOCACHE="$(GOCACHE)" $(GO) test ./...

start: dev

dev:
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) run ./cmd/foxlab $(RUN_ARGS)

smoke:
	GOCACHE="$(GOCACHE)" GOPROXY="$(GOPROXY)" $(GO) run ./cmd/foxlab --no-raw --width 90 --height 24 $(RUN_ARGS)
