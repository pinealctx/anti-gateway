# AntiGateway

[English](README.md) | [简体中文](README_CN.md)

A unified AI gateway that provides a standardized interface for multiple LLM providers. AntiGateway accepts requests in OpenAI and Anthropic API formats and routes them to various upstream providers with protocol conversion, load balancing, and multi-tenant support.

## Important Notice

Kiro and GitHub Copilot provider support in this project is **unofficial** and intended for personal testing/research use.
It may become invalid at any time due to upstream policy or protocol changes.
If any related implementation affects your rights or interests, please contact the maintainer (or open an issue), and the related code can be removed promptly.

## Features

- **Multi-Provider Support**: Route requests to Kiro (AWS Claude), OpenAI, GitHub Copilot, and Anthropic
- **Protocol Conversion**: Seamlessly convert between OpenAI, Anthropic, and CodeWhisperer formats
- **Load Balancing**: Five strategies - weighted random, round-robin, least-used, priority, and smart (latency-aware)
- **Multi-Tenant Management**: Per-key authentication, rate limiting (QPM/TPM), and usage tracking
- **Auto-Continuation**: Automatically continue truncated LLM responses
- **Output Sanitization**: Strip IDE-specific artifacts and perform identity replacement
- **Streaming Support**: Full SSE streaming for both OpenAI and Anthropic protocols
- **Web Admin UI**: React-based dashboard for managing keys, providers, and monitoring usage
- **Prometheus Metrics**: Built-in metrics for requests, latency, tokens, and errors

## Quick Start

### Prerequisites

- Go 1.23+
- Node.js 20+ (for frontend development)

### Installation

```bash
# Clone the repository
git clone https://github.com/pinealctx/anti-gateway.git
cd anti-gateway

# Build the server
go build -o antigateway .

# Copy and configure
cp config.example.yaml config.yaml
# Edit config.yaml with your settings

# Run
./antigateway
```

### Docker

```bash
# Build image
docker build -t antigateway .

# Run container
docker run -p 8080:8080 -v $(pwd)/config.yaml:/app/config.yaml antigateway
```

## Configuration

Create a `config.yaml` file based on `config.example.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  log_level: "info"      # debug | info | warn | error
  cors_origins: []       # Empty = allow all (dev mode)

auth:
  api_key: ""            # Bearer token for API auth (empty = disabled)
  admin_key: ""          # Separate key for /admin/* endpoints

defaults:
  provider: ""           # Fallback provider name
  model: "claude-sonnet-4-20250514"
  lb_strategy: "smart"   # weighted | round-robin | least-used | priority | smart

tenant:
  enabled: false         # Enable multi-tenant mode
  db_path: "antigateway.db"
```

## API Endpoints

### Chat Completions

**OpenAI Format**
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

**Anthropic Format**
```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Model Routing

Use model prefixes to route to specific providers:
- `openai/gpt-4o` → OpenAI provider
- `anthropic/claude-3-opus` → Anthropic provider
- `kiro/claude-sonnet-4-20250514` → Kiro provider

### Model Handling Policy

- Requests use **minimal normalization + passthrough**:
  - Empty model falls back to provider default model.
  - `claude-*-YYYYMMDD` date suffix is stripped when needed.
  - Other model names are forwarded as-is (no large alias remapping table).
- `/v1/models` returns an **outward-facing supported catalog** for Kiro/Copilot:
  - Includes maintained static model IDs.
  - Merges dynamic model IDs discovered from configured Copilot providers.
  - Includes both raw IDs and provider-prefixed IDs (such as `kiro/...`, `copilot/...`) for routing convenience.

### Other Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/models` | GET | List available models |
| `/v1/embeddings` | POST | Generate embeddings (OpenAI format) |
| `/health` | GET | Health check |
| `/metrics` | GET | Prometheus metrics |
| `/ui` | GET | Web admin UI |

## Admin API

Providers are managed dynamically via the Admin API:

```bash
# List providers
curl http://localhost:8080/admin/providers \
  -H "Authorization: Bearer ADMIN_KEY"

# Add a provider
curl -X POST http://localhost:8080/admin/providers \
  -H "Authorization: Bearer ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "openai-main",
    "type": "openai",
    "api_key": "sk-...",
    "weight": 100,
    "enabled": true
  }'

# Update a provider
curl -X PUT http://localhost:8080/admin/providers/1 \
  -H "Authorization: Bearer ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"weight": 50}'

# Delete a provider
curl -X DELETE http://localhost:8080/admin/providers/1 \
  -H "Authorization: Bearer ADMIN_KEY"
```

### API Key Management (Multi-Tenant Mode)

```bash
# Create API key
curl -X POST http://localhost:8080/admin/keys \
  -H "Authorization: Bearer ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "dev-team",
    "qpm_limit": 60,
    "tpm_limit": 100000
  }'

# List keys
curl http://localhost:8080/admin/keys \
  -H "Authorization: Bearer ADMIN_KEY"

# Get usage statistics
curl http://localhost:8080/admin/usage \
  -H "Authorization: Bearer ADMIN_KEY"
```

## Provider Types

### Kiro (AWS Claude)

Uses PKCE authentication flow. Configure via admin endpoints:

```bash
# Initiate Kiro login
curl -X POST http://localhost:8080/admin/kiro/login \
  -H "Authorization: Bearer ADMIN_KEY"

# Check login status
curl http://localhost:8080/admin/kiro/login/:id \
  -H "Authorization: Bearer ADMIN_KEY"

# Complete authentication
curl -X POST http://localhost:8080/admin/kiro/login/complete/:id \
  -H "Authorization: Bearer ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"code": "AUTH_CODE", "state": "STATE"}'
```

### GitHub Copilot

Uses device flow authentication:

```bash
# Start device flow
curl -X POST http://localhost:8080/admin/auth/device-code \
  -H "Authorization: Bearer ADMIN_KEY"

# Poll for token (after user authorizes)
curl http://localhost:8080/admin/auth/poll/:id \
  -H "Authorization: Bearer ADMIN_KEY"
```

### OpenAI

Standard API key authentication:

```json
{
  "name": "openai",
  "type": "openai",
  "api_key": "sk-...",
  "base_url": "https://api.openai.com/v1"
}
```

### Anthropic

Direct Anthropic API:

```json
{
  "name": "anthropic",
  "type": "anthropic",
  "api_key": "sk-ant-...",
  "base_url": "https://api.anthropic.com"
}
```

## Load Balancing Strategies

| Strategy | Description |
|----------|-------------|
| `weighted` | Random selection weighted by provider weight |
| `round-robin` | Sequential rotation through providers |
| `least-used` | Select provider with lowest request count |
| `priority` | Always select highest weight provider |
| `smart` | Score-based: combines weight, recent 429 errors, and average latency |

## Development

### Building

```bash
# Format code
make fmt

# Run linter
make lint

# Build
make build

# Run tests
make test

# Run all checks
make check

# Build with injected version
make build VERSION=v0.3.0
```

Version is injected at build time via ldflags:

```bash
go build -ldflags "-X github.com/pinealctx/anti-gateway/config.Version=v0.3.0" -o antigateway .
```

### Frontend Development

```bash
cd frontend
npm install
npm run dev    # Start dev server with hot reload
npm run build  # Build for production (outputs to web/static)
```

### Pre-commit Hooks

```bash
make setup-hooks
```

This installs hooks for:
- gitleaks (secret scanning)
- go fmt
- golangci-lint
- build verification
- test execution

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Clients                               │
│              (OpenAI SDK / Anthropic SDK / curl)            │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                     AntiGateway                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Auth      │  │  Rate       │  │    Protocol         │  │
│  │ Middleware  │──│  Limiter    │──│    Converter        │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│                          │                                   │
│                          ▼                                   │
│  ┌─────────────────────────────────────────────────────┐    │
│  │              Provider Registry                       │    │
│  │         (Load Balancing + Health Checks)            │    │
│  └─────────────────────────────────────────────────────┘    │
└─────────────────────────┬───────────────────────────────────┘
                          │
          ┌───────────────┼───────────────┬───────────────┐
          ▼               ▼               ▼               ▼
     ┌─────────┐    ┌─────────┐    ┌─────────┐    ┌─────────┐
     │  Kiro   │    │ OpenAI  │    │ Copilot │    │Anthropic│
     └─────────┘    └─────────┘    └─────────┘    └─────────┘
```

## Metrics

Prometheus metrics available at `/metrics`:

- `antigateway_requests_total` - Total requests by provider, model, status
- `antigateway_request_duration_seconds` - Request latency histogram
- `antigateway_tokens_total` - Token usage by provider, model, type (input/output)
- `antigateway_provider_health` - Provider health status (1=healthy, 0=unhealthy)
- `antigateway_rate_limit_hits_total` - Rate limit violations

## Acknowledgements

Thanks to the following related projects:

- [AntiHub-ALL](https://github.com/zhongruan0522/AntiHub-ALL) - Reference for Kiro provider implementation
- [copilot2api-go](https://github.com/StarryKira/copilot2api-go) - GitHub Copilot provider implementation reference

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
