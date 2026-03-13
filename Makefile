.PHONY: fmt lint build test check setup-hooks

fmt:
	go fmt ./...

lint:
	golangci-lint run

build:
	go build ./...

test:
	go test ./...

check: fmt lint build test

setup-hooks:
	@cp scripts/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit 2>/dev/null || true
	@echo "pre-commit hook installed"
