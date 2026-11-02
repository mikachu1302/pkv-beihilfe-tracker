export PATH := $(HOME)/go/bin:$(PATH)

WAILS  ?= $(shell command -v wails3 2>/dev/null || echo $(HOME)/go/bin/wails3)
# Linux builds against GTK3/WebKitGTK 4.1; other systems need no tag.
TAGS   ?= $(if $(filter Linux,$(shell uname -s)),gtk3,)
PREFIX ?= $(HOME)/.local
APP     = pkv-beihilfe-tracker

BIN_DIR  = $(PREFIX)/bin
APP_DIR  = $(PREFIX)/share/applications
ICON_DIR = $(PREFIX)/share/icons/hicolor/512x512/apps

.PHONY: dev build run test vet lint vulncheck bindings frontend-check check dist install uninstall demo clean

dev:       ## run with hot reload
	$(WAILS) task dev

build:     ## build bin/pkv-beihilfe-tracker for this OS
	$(WAILS) task build

run: build
	./bin/$(APP)

# main.go embeds frontend/dist; an empty placeholder is enough for Go checks.
dist:
	@mkdir -p frontend/dist && [ -e frontend/dist/index.html ] || echo '<!doctype html>' > frontend/dist/index.html

test: dist ## Go tests
	go test $(if $(TAGS),-tags $(TAGS)) ./...

vet: dist
	go vet $(if $(TAGS),-tags $(TAGS)) ./...

lint: dist ## golangci-lint (install: https://golangci-lint.run)
	golangci-lint run

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest $(if $(TAGS),-tags $(TAGS)) ./...

bindings:  ## regenerate the TypeScript bindings
	$(WAILS) generate bindings -f '$(if $(TAGS),-tags $(TAGS))' -clean=true -ts -i

frontend-check: bindings
	cd frontend && npx tsc --noEmit && npx vitest run

check: dist frontend-check ## what CI runs (lint only when installed)
	test -z "$$(gofmt -l . | grep -v node_modules)" || (gofmt -l . ; exit 1)
	go vet $(if $(TAGS),-tags $(TAGS)) ./...
	go test -race $(if $(TAGS),-tags $(TAGS)) ./...
	-command -v golangci-lint >/dev/null && golangci-lint run

install: build ## Linux: install for the current user (no root needed)
	install -Dm755 bin/$(APP) $(BIN_DIR)/$(APP)
	install -Dm644 build/appicon.png $(ICON_DIR)/$(APP).png
	mkdir -p $(APP_DIR)
	sed -e 's|^Exec=.*|Exec=$(BIN_DIR)/$(APP)|' -e 's|^Icon=.*|Icon=$(APP)|' -e 's|^Name=.*|Name=PKV Beihilfe Tracker|' \
		build/linux/$(APP).desktop > $(APP_DIR)/$(APP).desktop
	chmod 644 $(APP_DIR)/$(APP).desktop
	-update-desktop-database $(APP_DIR) >/dev/null 2>&1
	-gtk-update-icon-cache -q -t $(PREFIX)/share/icons/hicolor >/dev/null 2>&1
	@echo "Installed. Start 'PKV Beihilfe Tracker' from the application menu."

uninstall:
	rm -f $(BIN_DIR)/$(APP) $(APP_DIR)/$(APP).desktop $(ICON_DIR)/$(APP).png
	-update-desktop-database $(APP_DIR) >/dev/null 2>&1

demo:      ## make demo DIR=/tmp/pkv-demo – a data folder with made-up invoices
	go run ./cmd/demodata $(DIR)

clean:
	rm -rf bin frontend/dist frontend/bindings
