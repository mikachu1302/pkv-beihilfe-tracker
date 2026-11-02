export PATH := $(HOME)/go/bin:$(PATH)

WAILS  ?= $(shell command -v wails3 2>/dev/null || echo $(HOME)/go/bin/wails3)
UNAME  := $(shell uname -s)
# Linux builds against GTK3/WebKitGTK 4.1; other systems need no tag.
TAGS   ?= $(if $(filter Linux,$(UNAME)),gtk3,)
PREFIX ?= $(HOME)/.local
APP     = pkv-beihilfe-tracker
APP_NAME = PKV Beihilfe Tracker

DIST_LINUX = dist/$(APP)-linux-$(shell go env GOARCH)
MAC_APP    = $(HOME)/Applications/$(APP_NAME).app

.PHONY: dev build run test vet lint vulncheck bindings frontend-check check dist dist-linux install uninstall demo clean

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
	cd frontend && npx tsc --noEmit && npm run lint && npx vitest run

check: dist frontend-check ## what CI runs (linters only when installed)
	test -z "$$(gofmt -l . | grep -v node_modules)" || (gofmt -l . ; exit 1)
	go vet $(if $(TAGS),-tags $(TAGS)) ./...
	go test -race $(if $(TAGS),-tags $(TAGS)) ./...
	-command -v golangci-lint >/dev/null && golangci-lint run
	-command -v shellcheck >/dev/null && shellcheck build/linux/install.sh build/linux/nfpm/scripts/postinstall.sh scripts/install.sh
	-command -v govulncheck >/dev/null && govulncheck $(if $(TAGS),-tags $(TAGS)) ./...

# Linux release folder + archive: binary, icon, menu entry and install.sh
# (the same files `make install` uses). Builds first if there is no binary.
dist-linux:
	@test -x bin/$(APP) || $(MAKE) build
	rm -rf $(DIST_LINUX) $(DIST_LINUX).tar.gz
	mkdir -p $(DIST_LINUX)
	cp bin/$(APP) build/linux/install.sh $(DIST_LINUX)/
	cp build/appicon.svg $(DIST_LINUX)/$(APP).svg
	cp build/linux/$(APP).desktop $(DIST_LINUX)/
	tar -czf $(DIST_LINUX).tar.gz -C dist $(notdir $(DIST_LINUX))
	@echo "$(DIST_LINUX).tar.gz"

install: ## install for the current user: Linux ~/.local (no root), macOS ~/Applications
ifeq ($(UNAME),Darwin)
	$(WAILS) task darwin:package
	mkdir -p "$(HOME)/Applications"
	rm -rf "$(MAC_APP)"
	cp -R bin/$(APP).app "$(MAC_APP)"
	@echo "Installiert: $(MAC_APP)"
else
	$(MAKE) build
	$(MAKE) dist-linux
	PREFIX="$(PREFIX)" sh $(DIST_LINUX)/install.sh
endif

uninstall: ## remove the app (the data folder stays)
ifeq ($(UNAME),Darwin)
	rm -rf "$(MAC_APP)"
else
	PREFIX="$(PREFIX)" sh build/linux/install.sh --uninstall
endif

demo:      ## make demo DIR=/tmp/pkv-demo – a data folder with made-up invoices
	go run ./cmd/demodata $(DIR)

clean:
	rm -rf bin dist frontend/dist frontend/bindings
