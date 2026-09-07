# Minecraft Instance Manager
#
# The CLI and the GUI are separate binaries on purpose: Gio needs cgo on Linux
# and macOS, and keeping it out of the CLI is what lets the CLI cross-compile
# for every target from one host with CGO_ENABLED=0.

VERSION      ?= v2.0.0-dev
MSA_CLIENT_ID ?=

CLI_BIN  := minecraft-instance-manager
GUI_BIN  := minecraft-instance-manager-gui
# LEGACY_BIN is the name the GUI used to be installed under as well. Mojang's
# own package owns that name in /usr/bin, and ~/.local/bin comes first on PATH,
# so the copy shadowed the official launcher for anyone typing it in a shell.
# Nothing installs it any more; uninstall still removes it.
LEGACY_BIN := minecraft-launcher

PREFIX      ?= $(HOME)/.local
BINDIR      := $(PREFIX)/bin
APPDIR      := $(PREFIX)/share/applications
ICONDIR     := $(PREFIX)/share/icons/hicolor
DESKTOP_ID  := minecraft-instance-manager

LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.defaultMSAClientID=$(MSA_CLIENT_ID)

# Gio prefers Vulkan and needs its headers to build. Where they are missing —
# vulkan-headers on Arch, libvulkan-dev on Debian — fall back to the OpenGL
# backend rather than failing the build.
VULKAN_HEADER := /usr/include/vulkan/vulkan.h
ifeq ($(wildcard $(VULKAN_HEADER)),)
GUI_TAGS := -tags novulkan
GUI_BACKEND := OpenGL (install vulkan headers for the Vulkan backend)
else
GUI_TAGS :=
GUI_BACKEND := Vulkan
endif

.PHONY: all build cli gui install install-desktop uninstall test lint clean run

all: build

build: cli gui

cli:
	@echo "  building $(CLI_BIN) (CGO_ENABLED=0)"
	@CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o dist/$(CLI_BIN) ./cmd/minecraft-instance-manager

gui:
	@echo "  building $(GUI_BIN) [$(GUI_BACKEND)]"
	@go build $(GUI_TAGS) -ldflags="$(LDFLAGS)" -o dist/$(GUI_BIN) ./cmd/minecraft-instance-manager-gui

# install puts both binaries on PATH and registers the GUI as a desktop
# application, so the launcher is reachable from the application grid rather
# than only from a terminal. It is idempotent and safe to run after every build.
install: build install-desktop
	@install -Dm755 dist/$(CLI_BIN) $(BINDIR)/$(CLI_BIN)
	@install -Dm755 dist/$(GUI_BIN) $(BINDIR)/$(GUI_BIN)
	@rm -f $(BINDIR)/$(LEGACY_BIN)
	@echo "  installed to $(BINDIR)"

install-desktop:
	@install -d $(APPDIR) $(ICONDIR)/scalable/apps
	@sed 's|@BINDIR@|$(BINDIR)|g' packaging/$(DESKTOP_ID).desktop.in > $(APPDIR)/$(DESKTOP_ID).desktop
	@install -Dm644 packaging/$(DESKTOP_ID).svg $(ICONDIR)/scalable/apps/$(DESKTOP_ID).svg
	@desktop-file-validate $(APPDIR)/$(DESKTOP_ID).desktop 2>/dev/null || true
	@update-desktop-database $(APPDIR) 2>/dev/null || true
	@gtk-update-icon-cache -f -t $(ICONDIR) 2>/dev/null || true
	@echo "  registered $(DESKTOP_ID).desktop"

uninstall:
	@rm -f $(BINDIR)/$(CLI_BIN) $(BINDIR)/$(GUI_BIN) $(BINDIR)/$(LEGACY_BIN)
	@rm -f $(APPDIR)/$(DESKTOP_ID).desktop
	@rm -f $(ICONDIR)/scalable/apps/$(DESKTOP_ID).svg
	@update-desktop-database $(APPDIR) 2>/dev/null || true
	@gtk-update-icon-cache -f -t $(ICONDIR) 2>/dev/null || true
	@echo "  removed"

# All the launcher logic lives in internal/launcher, which has no Gio
# dependency, so the suite runs on a machine with no display at all.
test:
	@go test $(GUI_TAGS) -race ./...

lint:
	@gofmt -l . | grep -v '^$$' && exit 1 || true
	@go vet $(GUI_TAGS) ./...

run: gui
	@./dist/$(GUI_BIN)

clean:
	@rm -rf dist
