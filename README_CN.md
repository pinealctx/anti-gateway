# AntiGateway

[English](README.md) | [简体中文](README_CN.md)

统一的 AI 网关，提供标准化接口接入多个 LLM 提供方。AntiGateway 支持 OpenAI 与 Anthropic 两种 API 格式，并可按规则路由到不同上游，包含协议转换、负载均衡与多租户能力。

## 重要声明

本项目中的 Kiro 与 GitHub Copilot 支持为非官方支持，仅用于个人测试与研究。
受上游策略或协议变化影响，相关能力可能随时失效。
如果相关实现影响到任何权利或利益，请联系维护者（或提交 issue），可及时移除相关代码。

## 核心功能

- **多提供方接入**：Kiro（AWS Claude）、OpenAI、GitHub Copilot、Anthropic
- **协议转换**：OpenAI、Anthropic、CodeWhisperer 格式互转
- **负载均衡**：weighted、round-robin、least-used、priority、smart 五种策略
- **多租户管理**：按 Key 鉴权、QPM/TPM 限流、用量统计
- **自动续写**：自动续写被截断的 LLM 响应
- **输出清洗**：移除 IDE 特定产物，执行身份替换
- **流式响应**：完整 SSE 流式支持
- **Web 管理后台**：React 管理面板，管理密钥、提供方、监控使用情况
- **Prometheus 指标**：请求、延迟、Token、错误监控

## 快速开始

### 依赖

- Go 1.23+
- Node.js 20+（前端开发）

### 构建运行

```bash
git clone https://github.com/pinealctx/anti-gateway.git
cd anti-gateway

# 构建
go build -o antigateway .

# 配置
cp config.example.yaml config.yaml
# 编辑 config.yaml

# 运行
./antigateway
```

### Docker

```bash
docker build -t antigateway .
docker run -p 8080:8080 -v $(pwd)/config.yaml:/app/config.yaml antigateway
```

## 配置说明

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  log_level: "info"
  cors_origins: []       # 空 = 允许所有

auth:
  api_key: ""            # API 认证令牌（空 = 禁用）
  admin_key: ""          # /admin/* 端点专用密钥

defaults:
  provider: ""           # 默认提供方
  model: "claude-sonnet-4-20250514"
  lb_strategy: "smart"   # weighted | round-robin | least-used | priority | smart

tenant:
  enabled: false         # 启用多租户模式
  db_path: "antigateway.db"
```

## API 端点

### 聊天补全

**OpenAI 格式**
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'
```

**Anthropic 格式**
```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

### 模型路由

使用前缀路由到指定提供方：
- `openai/gpt-4o` → OpenAI 提供方
- `anthropic/claude-3-opus` → Anthropic 提供方
- `kiro/claude-sonnet-4-20250514` → Kiro 提供方

### 其他端点

| 端点 | 方法 | 说明 |
|------|------|------|
| `/v1/models` | GET | 列出可用模型 |
| `/v1/embeddings` | POST | 生成向量（OpenAI 格式） |
| `/health` | GET | 健康检查 |
| `/metrics` | GET | Prometheus 指标 |
| `/ui` | GET | Web 管理界面 |

## 开发

### 构建

```bash
make fmt        # 格式化
make lint       # 代码检查
make build      # 构建
make test       # 测试
make check      # 全部检查
```

版本注入：

```bash
go build -ldflags "-X github.com/pinealctx/anti-gateway/config.Version=v0.3.0" -o antigateway .
```

### 前端开发

```bash
cd frontend
npm install
npm run dev    # 开发服务器
npm run build  # 生产构建（输出到 web/static）
```

### Pre-commit Hooks

```bash
make setup-hooks
```

## 负载均衡策略

| 策略 | 说明 |
|------|------|
| `weighted` | 按权重随机选择 |
| `round-robin` | 轮询 |
| `least-used` | 选择请求量最少的提供方 |
| `priority` | 始终选择权重最高的提供方 |
| `smart` | 综合权重、429 错误率、平均延迟评分选择 |

## 架构

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

## 指标

`/metrics` 端点提供 Prometheus 指标：

- `antigateway_requests_total` - 按提供方、模型、状态的请求总数
- `antigateway_request_duration_seconds` - 请求延迟直方图
- `antigateway_tokens_total` - 按提供方、模型、类型的 Token 使用量
- `antigateway_provider_health` - 提供方健康状态
- `antigateway_rate_limit_hits_total` - 限流触发次数

## 致谢

感谢以下相关项目：

- [AntiHub-ALL](https://github.com/zhongruan0522/AntiHub-ALL) - Kiro 提供方实现参考
- [copilot2api-go](https://github.com/StarryKira/copilot2api-go) - GitHub Copilot 提供方实现参考

## 协议

本项目基于 MIT 协议开源，详见 [LICENSE](LICENSE)。

## 贡献

欢迎贡献！请提交 issue 或 pull request。
