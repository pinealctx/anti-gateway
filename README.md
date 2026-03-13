# AntiGateway

A unified AI gateway that provides a standardized interface for multiple LLM providers. AntiGateway accepts requests in OpenAI and Anthropic API formats and routes them to various upstream providers with protocol conversion, load balancing, and multi-tenant support.

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

- Go 1.25+
- Node.js 20+ (for frontend development)

### Installation

```bash
# Clone the repository
git clone https://github.com/pinealctx/anti-gateway.git
cd anti-gateway

# Build the server
go build -o antigateway ./cmd/server

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
  model: "claude-opus-4.6"
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
    "model": "claude-opus-4.6",
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
    "model": "claude-opus-4.6",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Model Routing

Use model prefixes to route to specific providers:
- `openai/gpt-4o` → OpenAI provider
- `anthropic/claude-3-opus` → Anthropic provider
- `kiro/claude-opus-4.6` → Kiro provider

### Other Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/models` | GET | List available models |
| `/v1/embeddings` | POST | Generate embeddings (OpenAI format) |
| `/health` | GET | Health check |
| `/metrics` | GET | Prometheus metrics |

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

# Complete authentication with code
curl -X POST http://localhost:8080/admin/kiro/callback \
  -H "Authorization: Bearer ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"code": "AUTH_CODE"}'
```

### GitHub Copilot

Uses device flow authentication:

```bash
# Start device flow
curl -X POST http://localhost:8080/admin/copilot/device-code \
  -H "Authorization: Bearer ADMIN_KEY"

# Poll for token (after user authorizes)
curl -X POST http://localhost:8080/admin/copilot/token \
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
```

### Frontend Development

```bash
cd web
npm install
npm run dev    # Start dev server with hot reload
npm run build  # Build for production (outputs to internal/web/static)
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

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
