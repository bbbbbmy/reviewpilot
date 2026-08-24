# ReviewPilot

> 自托管 AI 代码审查机器人（Go + CLI + Web UI）。当前 v0.4.0 — MVP 已完成。

ReviewPilot 监听 GitHub Pull Request，运行 5 步审查流水线调用任意
OpenAI 兼容的大语言模型，将行内评论与总结评论回写到 GitHub，
并提供一个 Web 界面供团队标记结论、把反馈喂给下一次审查。

[English version](README.md)

| 组件 | 状态 |
|---|---|
| `internal/llmopenai` | ✅ 已发布 (v0.1.0) |
| `internal/reviewengine` | ✅ 已发布 (v0.2.0) |
| `internal/platformgithub` | ✅ 已发布 (v0.3.0) |
| `internal/webdashboard` | ✅ 已发布 (v0.4.0) |
| CLI (`reviewpilot review`) | ✅ 已发布 (v0.3.0) |
| Docker 镜像 | ✅ 已发布 (v0.0.1，每次发布重建) |
| GitHub Actions CI | ✅ main 分支绿（134 测试） |

每个版本的具体变更见 [`CHANGELOG.md`](CHANGELOG.md)。

## 为什么做这个

你完全可以在 GitHub 上跑 **CodeRabbit**，但它是闭源 SaaS。
如果你想要一个代码审查机器人满足以下条件：

- 跑在**你自己的服务器**上（代码永远不离开你的内网）
- 能**自由切换 LLM** 到任意 OpenAI 兼容端点（DeepSeek、月之暗面、智谱、本地 Ollama 等）
- 把审查状态存在**单个 SQLite 文件**里方便备份
- 让团队**对评论点赞/点踩**并把信号反馈到下一次审查

那么 ReviewPilot 是最小可用的实现。

## 快速上手（Docker Compose）

```yaml
# /path/to/reviewpilot/docker-compose.yml
services:
  reviewpilot:
    build: .
    image: bbbbbmy/reviewpilot:v0.4.0
    container_name: reviewpilot
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      # 必填 — 缺一启动失败
      RP_GITHUB_WEBHOOK_SECRET: "你的-webhook-secret"
      RP_GITHUB_TOKEN: "ghp_xxx"
      RP_ADMIN_USER: admin
      RP_ADMIN_PASS: "你的-admin-密码"

      # 选填但建议
      RP_HTTP_LISTEN: ":8080"
      RP_DB_PATH: /var/lib/reviewpilot/review.db
      RP_LLM_OPENAI_API_KEY: "sk-xxx"
      # RP_LLM_OPENAI_BASE_URL: "https://api.deepseek.com/v1"   # 用非 OpenAI 提供方时
    volumes:
      - data:/var/lib/reviewpilot
    healthcheck:
      test: ["CMD", "/usr/local/bin/reviewpilot", "--healthcheck-only"]
      interval: 30s
      timeout: 5s
      retries: 3

volumes:
  data:
```

然后：

```bash
docker compose up -d
curl http://localhost:8080/healthz
# {"status":"ok"}
```

浏览器打开 `http://localhost:8080`，会要求输入 Basic Auth 凭据（即上方
`RP_ADMIN_USER` / `RP_ADMIN_PASS`）。

完整生产部署教程（HTTPS、反向代理、GitHub App 注册、生产密钥管理）见
[`docs/deployment.md`](docs/deployment.md)。

## CLI：本地审查 git diff

```bash
# 构建
go build -o bin/reviewpilot ./cmd/reviewpilot

# 审查 main..feature，使用配置的 LLM
export RP_LLM_OPENAI_API_KEY="sk-..."
./bin/reviewpilot review main..feature --repo bbbbbmy/reviewpilot --out out.json

# 从 stdin 读 patch（CI 场景）
git diff main..feature | ./bin/reviewpilot review --stdin --repo bbbbbmy/reviewpilot
```

输出 JSON 格式：

```json
{
  "status": "done",
  "comments": [
    {
      "file_path": "internal/x.go",
      "line_number": 42,
      "body": "这个 nil 检查在 early return 之后多余",
      "fingerprint": "...",
      "posted_to_platform": false
    }
  ],
  "tokens_in": 200,
  "tokens_out": 30
}
```

CLI 模式**不会**把评论 post 到 GitHub — 它只跑管线并打印评论。
若要驱动 GitHub 评论，请跑 server 模式并发送 webhook。

## Server 模式

`./bin/reviewpilot server`（默认子命令；不传 subcommand 等价）启动监听 `:8080`
的 HTTP server，接收 GitHub `pull_request` webhook。

| 路径 | 鉴权 | 说明 |
|---|---|---|
| `POST /webhook` | GitHub HMAC 签名 | 接收 PR 事件 → 跑管线 → post 行内 + 总结评论 |
| `GET /healthz` | 无 | 存活探针（`{"status":"ok"}`） |
| `GET /metrics` | 无 | Prometheus 指标（审查流水线 counters + histograms） |
| `GET /` | Basic Auth | 重定向到 `/reviews` |
| `GET /reviews` | Basic Auth | 最近审查列表 HTML |
| `GET /reviews/{id}` | Basic Auth | 单审查详情 HTML（含评论 + 👍/👎） |
| `GET /repos` | Basic Auth | 已配置仓库列表 + upsert 表单 HTML |
| `POST /repos` | Basic Auth | 创建 / 更新仓库配置 |
| `POST /reviews/{id}/comments/{cid}/feedback` | Basic Auth | 提交 👍/👎 + 文本 |
| `GET /api/v1/repos` | Basic Auth | JSON 列表（CLI / 第三方用） |
| `POST /api/v1/repos` | Basic Auth | JSON upsert |
| `GET /api/v1/reviews` | Basic Auth | JSON 列表（`?repo=`、`?status=` 过滤） |
| `GET /api/v1/reviews/{id}` | Basic Auth | JSON 详情 + 评论 |
| `POST /api/v1/reviews/{id}/comments/{cid}/feedback` | Basic Auth | JSON 反馈提交 |

HTTP API 在 `/api/v1/...` 前缀下保持稳定，跨越 v0.x；破坏性变更需要新主版本号。

## 配置

| 环境变量 | 必填 | 说明 |
|---|---|---|
| `RP_ADMIN_USER` / `RP_ADMIN_PASS` | 是 | Web UI 和 JSON API 的 Basic Auth 凭据。**服务缺一启动失败**（或 `RP_DEV_MODE=1` 跳过）。 |
| `RP_GITHUB_WEBHOOK_SECRET` | 是（server） | GitHub 用来签 `X-Hub-Signature-256` header 的共享密钥。 |
| `RP_GITHUB_TOKEN` | 是（server） | 出站 GitHub API 的 bearer token。用 fine-grained PAT（scope 到要审查的 repo），或 GitHub App installation token。 |
| `RP_GITHUB_APP_ID` / `RP_GITHUB_APP_PRIVATE_KEY` | 保留 | 为 GitHub App JWT 交换预留（v0.4.0 未启用）。 |
| `RP_GITHUB_BASE_URL` | 否 | 默认 `https://api.github.com`。GitHub Enterprise 时覆盖。 |
| `RP_LLM_OPENAI_API_KEY` | 是 | LLM 提供方 API key。 |
| `RP_LLM_OPENAI_BASE_URL` | 否 | 默认 OpenAI 官方。设为 `https://api.deepseek.com/v1`、`https://api.moonshot.cn/v1` 等。 |
| `RP_LLM_OPENAI_MODEL` | 否 | 默认 `gpt-4o-mini`。 |
| `RP_HTTP_LISTEN` | 否 | 默认 `:8080`。 |
| `RP_DB_PATH` | 否 | 默认 `/var/lib/reviewpilot/review.db`。 |
| `RP_DEV_MODE` | 仅开发 | 设为 `1` 跳过 Basic Auth（生产禁用）。 |

详见 [`docs/configuration.md`](docs/configuration.md)。

## 架构（1 分钟速览）

```
                ┌──────────────────┐
                │  GitHub webhook  │
                │  (PR opened /    │
                │  synchronize)    │
                └─────────┬────────┘
                          │ HMAC 签名
                          ▼
┌─────────────────────────────────────────────────┐
│           ReviewPilot server (:8080)            │
│                                                 │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐   │
│  │ cmd/      │───▶│ review-  │───▶│ llmopenai│   │
│  │ reviewpilot│    │ engine   │    │ OpenAI   │   │
│  │ (cobra)   │    │ pipeline │    │ provider │   │
│  └─────┬────┘    └─────┬────┘    └─────┬────┘   │
│        │               │                │       │
│        │               │ dedup          │       │
│        │               ▼                ▼       │
│        │         ┌──────────┐         token  │
│        │         │ platform-│          usage   │
│        │         │ github   │                  │
│        │         │ adapter  │                  │
│        │         └─────┬────┘                  │
│        ▼               ▼                       │
│  ┌──────────┐    ┌──────────┐                  │
│  │ webdash- │    │ SQLite   │                  │
│  │ board    │◀──▶│ store    │                  │
│  │ (HTML +  │    │ (repo /  │                  │
│  │  API +   │    │ review / │                  │
│  │  auth)   │    │ comment /│                  │
│  └─────┬────┘    │ feedback)│                  │
│        │         └──────────┘                  │
│        ▼                                        │
│  ┌──────────┐                                   │
│  │ Basic    │                                   │
│  │ Auth     │                                   │
│  └──────────┘                                   │
└─────────────────────────────────────────────────┘
```

完整架构（组件边界、设计决策、备选方案）见 [`docs/architecture.md`](docs/architecture.md)。

## 开发

```bash
# Clone + 装 toolchain
go version  # 需要 1.25 或更新（go.mod 声明 1.25.0）

# 完整检查
gofmt -l .                                  # 应为空
go vet ./...                                # 应为空
go install honnef.co/go/tools/cmd/staticcheck@v0.7.0
staticcheck ./...                           # 应为空
go test -count=1 -race ./...                # 134 测试，约 1 秒
```

GitHub Actions 在每次 push 到 `main` 时跑同一套检查。
见 `.github/workflows/ci.yml`。

## 项目结构

```
bbbbbmy-reviewpilot/
├── cmd/reviewpilot/        # cobra 入口 + server + CLI 子命令
├── internal/
│   ├── llmopenai/          # OpenAI 兼容 LLM 客户端 + 错误归一
│   ├── reviewengine/       # 5 步管线 + dedup + feedback + metrics
│   ├── platformgithub/     # GitHub App webhook + 适配器 + 限速
│   └── webdashboard/       # HTML + JSON API + auth + SQLite
│       ├── migrations/      # 嵌入式 SQL schema
│       ├── templates/       # html/template HTML
│       └── static/          # CSS
├── docs/                   # architecture / deployment / config / 等
├── Dockerfile              # multi-stage → distroless static-debian12
├── docker-compose.yml      # 一文件部署
├── .github/workflows/      # lint / test / docker build
└── CHANGELOG.md            # 发布历史
```

每个包的行为契约在
[`vibecoding → openspec/specs/`](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs)。
如果你要改行为，先通过 OpenSpec 提案改 spec。

## 运维文档

- [`docs/architecture.md`](docs/architecture.md) — 数据流、组件边界、决策表
- [`docs/configuration.md`](docs/configuration.md) — 每个环境变量详解
- [`docs/deployment.md`](docs/deployment.md) — 生产部署、TLS、反向代理、备份
- [`docs/cli.md`](docs/cli.md) — `reviewpilot review` flag + JSON 输出格式
- [`docs/webhook-setup.md`](docs/webhook-setup.md) — 注册 GitHub App
- [`docs/operations.md`](docs/operations.md) — 备份 / 升级 / 监控 / 故障排查

## 许可证

[MIT](LICENSE)
