# AntiGateway

[English](README.md) | [简体中文](README_CN.md)

统一的 AI 网关，提供标准化接口接入多个 LLM 提供方。AntiGateway 支持 OpenAI 与 Anthropic 两种 API 格式，并可按规则路由到不同上游，包含协议转换、负载均衡与多租户能力。

## 重要声明

本项目中的 Kiro 与 GitHub Copilot 支持为非官方支持，仅用于个人测试与研究。
受上游策略或协议变化影响，相关能力可能随时失效。
如果相关实现影响到任何权利或利益，请联系维护者（或提交 issue），可及时移除相关代码。

## 核心功能

- 多提供方接入：Kiro（AWS Claude）、OpenAI、GitHub Copilot、Anthropic
- 协议转换：OpenAI、Anthropic、CodeWhisperer 格式互转
- 负载均衡：weighted、round-robin、least-used、priority、smart
- 多租户管理：按 Key 鉴权、QPM/TPM 限流、用量统计
- 流式响应：完整 SSE 支持
- Web 管理后台：管理提供方、密钥、使用情况
- Prometheus 指标：请求、延迟、Token、错误监控

## 快速开始

### 依赖

- Go 1.25+
- Node.js 20+（前端开发）

### 构建运行

```bash
git clone https://github.com/pinealctx/anti-gateway.git
cd anti-gateway

# 默认本地构建（版本为 dev）
make build

# 指定版本构建
make build VERSION=v0.3.0

cp config.example.yaml config.yaml
./antigateway
```

## 版本注入（ldflags）

项目不再在代码中硬编码发布版本号，发布版本通过构建参数注入：

```bash
go build -ldflags "-X github.com/pinealctx/anti-gateway/internal/config.Version=v0.3.0" -o antigateway ./cmd/server
```

## 自动发版

已配置 GitHub Actions：当推送 tag 且匹配 `v*.*.*` 时自动触发 release。
工作流会在编译阶段将 tag 值通过 ldflags 注入到二进制版本字段。

## 致谢

感谢以下相关项目：

- AntiHub-ALL: https://github.com/zhongruan0522/AntiHub-ALL
- copilot2api-go: https://github.com/StarryKira/copilot2api-go

## 开源协议兼容性说明

- 本项目（AntiGateway）为 MIT 协议。
- copilot2api-go 为 MIT，和本项目协议兼容。
- AntiHub-ALL 为 AGPL-3.0；仅做引用/致谢不会自动改变本项目 MIT 协议。
- 若未来复制或合并 AGPL 代码到本项目并进行分发，相关部分可能需要遵守 AGPL 义务。

## 文档导航

- 英文完整文档请查看 [README.md](README.md)
