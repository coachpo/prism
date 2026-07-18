# Prism

[English](README.md) | 简体中文

Prism 是一个自托管的网关,位于你的工具和 LLM 提供商之间,提供统一入口、集中的 API key 管理,以及一个可以查看每次请求花费的 Web 面板。它面向同时使用多家提供商的开发者和高级用户,让你无需搭建重型基础设施就能获得故障转移、路由和用量追踪。

一个 Go 二进制、一个 React 面板和一个 PostgreSQL 就是它的全部依赖。

## 功能

- 通过一个网关支持三种 API 风格:OpenAI(chat completions、responses、models)、Anthropic(messages)和 Gemini(generateContent),包含流式响应。
- 按模型名路由:对外的模型 ID 会依次解析一组有序目标,其中 *Terminal Target*(终端目标)是模型到具体提供商端点的最终绑定。可以在稳定的模型名背后切换或串联提供商。
- 使用 `single`、`fill-first` 或 `round-robin` 策略在端点间负载均衡,并自动重试。
- 应用*封禁策略*(ban policy):持续失败的端点会被暂时或永久(直到你手动重置)移出路由池,避免重试不断冲击已经失效的提供商。
- 将请求日志、token 用量和花费记录在 PostgreSQL 中,面板上可查看每个模型的成功率和延迟。
- 通过你按提供商定义的可复用定价模板(pricing template)为每次请求计费。
- 访问保护:面板可选启用操作员登录,代理调用方可选启用 API key;提供商密钥加密存储。
- 以一个 Docker 镜像加 PostgreSQL 的形式交付。

## 快速开始

### Docker Compose(推荐)

```bash
git clone https://github.com/coachpo/prism.git
cd prism
docker compose up -d --build
```

打开 http://localhost:8080。Compose 会构建应用镜像,在旁边运行 PostgreSQL 16,并将数据库和配置文件保存在命名卷中。`docker compose down` 保留数据;`docker compose down -v` 会删除数据。

常用的 `.env` 覆盖项包括 `PRISM_PUBLIC_PORT`、`POSTGRES_PASSWORD` 和 `BUILD_FRONTEND=false`(仅构建后端镜像)。凡是本地环境之外的部署,请务必修改默认数据库密码。

### 单镜像

根目录的 `Dockerfile` 构建一个包含 Go 后端、已构建面板和 Nginx 的镜像。PostgreSQL 不在镜像内,需要让容器指向你自己的实例:

```bash
docker build -t prism .
docker run -p 8080:8080 \
  -v prism_config:/app/config \
  -e PRISM_CONFIG_PATH=/app/config/config.json \
  -e DATABASE_URL="postgres://prism:prism@your-postgres:5432/prism?sslmode=disable" \
  prism
```

也提供分离的预构建镜像:`ghcr.io/coachpo/prism-backend` 和 `ghcr.io/coachpo/prism-frontend`。

### 本地开发

需要 Go 1.26.5、Node.js 24+、pnpm 和 Docker。后端和前端位于本 monorepo 的 `backend/` 与 `frontend/` 目录下。

```bash
./start.sh full      # 后端 + 前端开发服务器 + PostgreSQL
./start.sh headless  # 仅后端 + PostgreSQL
```

启动器在 `5173` 端口提供前端,在 `15432` 端口运行 PostgreSQL,后端默认监听 `8000`。子项目的工作流详见 [`backend/README.md`](backend/README.md) 和 [`frontend/README.md`](frontend/README.md)。

## 配置

Prism 从一个明文 JSON 文件启动(默认 `config.json`,路径由 `PRISM_CONFIG_PATH` 指定)。首次启动时会以默认值生成该文件,此后监听地址、数据库 URL、超时和密钥均以它为准。`DATABASE_URL` 只在首次启动时用于初始化数据库连接;之后以该文件为唯一事实来源。没有配置界面,也没有热加载——修改文件后需重启 Prism。

两个超时字段为必填并会自动生成:`runtime.transport.requestTimeout`(`"300s"`,上游整个请求的超时)和 `runtime.sideEffects.attemptTimeout`(`"10s"`,后台副作用单次尝试的预算)。

其余一切——模型、端点、负载均衡策略、定价模板、代理 key——都在面板中管理并存储于 PostgreSQL。数据库结构迁移在启动时自动执行。

备份实例的方式:用 `pg_dump` 导出数据库,并复制 `config.json`。

## 开发

```bash
# 后端
cd backend
go build ./cmd/prism-backend
go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...

# 前端
cd frontend
pnpm install
pnpm run dev
pnpm run lint
```

发布通过 `./release.sh` 进行(例如 `./release.sh patch --dry-run`),它会更新版本文件、打标签并触发镜像发布工作流。CI 以 `govulncheck` 和 `pnpm audit` 作为发布门禁。

## 文档

- [架构](docs/ARCHITECTURE.md)
- [API 规范](docs/API_SPEC.md)
- [数据模型](docs/DATA_MODEL.md)
- [工作流](docs/WORKFLOWS.md)
- [请求页说明](docs/REQUESTS_PAGE.md)
- [PRD](docs/PRD.md)

## 安全

Prism 面向可信的本地或局域网部署。虽然提供操作员登录和代理 API key,但没有通用的速率限制或滥用防护。请勿将 Prism 直接暴露在公网上;如需远程访问,请在前面加一层带认证的反向代理。
