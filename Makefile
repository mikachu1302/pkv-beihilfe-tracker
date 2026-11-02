export PATH := $(HOME)/go/bin:$(PATH)

.PHONY: test vet lint vulncheck check

test:      ## run Go tests
	go test ./...

vet:
	go vet ./...

lint:      ## golangci-lint (install: https://golangci-lint.run)
	golangci-lint run

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check:     ## what CI runs (lint only when installed)
	test -z "$$(gofmt -l .)" || (gofmt -l . ; exit 1)
	go vet ./...
	go test -race ./...
	-command -v golangci-lint >/dev/null && golangci-lint run
