# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2025-03-13

### Added

- Multi-provider support: Kiro (AWS Claude), OpenAI, GitHub Copilot, Anthropic
- Protocol conversion between OpenAI, Anthropic, and CodeWhisperer formats
- Five load balancing strategies: weighted, round-robin, least-used, priority, smart
- Multi-tenant mode with per-key authentication and rate limiting
- Sliding window rate limiter (QPM/TPM)
- Auto-continuation for truncated LLM responses
- Output sanitization (IDE artifact removal, identity replacement)
- Full SSE streaming support for OpenAI and Anthropic protocols
- Web admin UI (React + Ant Design)
- Prometheus metrics endpoint
- SQLite-backed persistence for providers and usage data
- Health check endpoints with automatic provider health tracking
- Model prefix routing (e.g., `openai/gpt-4o`)
- PKCE authentication flow for Kiro provider
- Device flow authentication for GitHub Copilot
- Pre-commit hooks for code quality

### Security

- Timing-safe HMAC for API key validation
- Separate admin key authentication
- CORS configuration support

## [0.1.0] - 2025-02-01

### Added

- Initial release
- Basic OpenAI-compatible API endpoint
- Single provider support
- Configuration via YAML file
