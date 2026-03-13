# Contributing to AntiGateway

Thank you for your interest in contributing to AntiGateway! This document provides guidelines and instructions for contributing.

## Code of Conduct

By participating in this project, you agree to maintain a respectful and inclusive environment for everyone.

## How to Contribute

### Reporting Bugs

Before submitting a bug report:

1. Check existing issues to avoid duplicates
2. Use the latest version to see if the issue persists
3. Collect relevant information (logs, config, environment)

When submitting a bug report, include:

- Clear, descriptive title
- Steps to reproduce the issue
- Expected vs actual behavior
- Environment details (OS, Go version, etc.)
- Relevant logs or error messages

### Suggesting Features

Feature requests are welcome! Please:

1. Check existing issues for similar suggestions
2. Clearly describe the use case
3. Explain why this feature would be useful
4. Consider potential implementation approaches

### Pull Requests

1. Fork the repository
2. Create a feature branch from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```
3. Make your changes
4. Run checks before committing:
   ```bash
   make check
   ```
5. Commit with clear, descriptive messages
6. Push to your fork and submit a pull request

## Development Setup

### Prerequisites

- Go 1.25+
- Node.js 20+ (for frontend)
- golangci-lint

### Getting Started

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/anti-gateway.git
cd anti-gateway

# Install pre-commit hooks
make setup-hooks

# Copy config
cp config.example.yaml config.yaml

# Run the server
go run ./cmd/server
```

### Frontend Development

```bash
cd web
npm install
npm run dev
```

## Code Style

### Go

- Follow standard Go conventions
- Run `go fmt` before committing
- Pass `golangci-lint` checks
- Write tests for new functionality
- Keep functions focused and small
- Use meaningful variable and function names

### TypeScript/React

- Follow existing code patterns
- Use TypeScript types appropriately
- Keep components focused
- Use functional components with hooks

## Testing

```bash
# Run all tests
make test

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/core/...
```

## Commit Messages

Use clear, descriptive commit messages:

```
feat: add support for new provider type
fix: resolve race condition in token refresh
docs: update API documentation
refactor: simplify load balancing logic
test: add tests for rate limiter
```

Prefixes:
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `test:` - Test additions or changes
- `chore:` - Maintenance tasks

## Project Structure

```
AntiGateway/
├── cmd/server/          # Application entrypoint
├── internal/
│   ├── api/             # HTTP handlers and routes
│   ├── config/          # Configuration loading
│   ├── core/            # Core business logic
│   │   ├── converter/   # Protocol converters
│   │   ├── providers/   # Provider abstraction
│   │   └── streaming/   # SSE handling
│   ├── middleware/      # HTTP middleware
│   ├── models/          # Data models
│   ├── providers/       # Provider implementations
│   └── tenant/          # Multi-tenant support
├── web/                 # Frontend source
└── docs/                # Documentation
```

## Review Process

1. All PRs require at least one review
2. CI checks must pass
3. Code coverage should not decrease significantly
4. Documentation should be updated if needed

## Questions?

Feel free to open an issue for any questions about contributing.

Thank you for contributing!
