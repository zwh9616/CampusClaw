# Bearer Token 无 Cookie 鉴权验收记录（2026-09-30）

## 范围与命令

- `docker run … golang:1.25-alpine go build ./... && go vet ./...`：通过。
- `docker run … campusclaw-zwh-test:latest go test -count=1 ./...`（对隔离测试库）：本变更涉及的包全部通过，详见下节。
- `cd frontend && npm run typecheck && npm test && npm run build`：通过，17 个用例全部通过。
- HTTP 验收入口（经 Nginx 8080，`tests/acceptance`）：28 checks，0 failed，退出码 0。
- 浏览器端到端（`node tests/browser/check.mjs`）：7 checks，0 failed，退出码 0。
- Vite 代理冒烟（`node tests/browser/vite-proxy.mjs`，Vite 5173 → Nginx 8080）：通过。
- `openspec validate switch-to-bearer-token-auth --strict`：通过。

## 后端结果

`docker compose up --build` 重建后，迁移 `0004_drop_refresh_id.sql` 已应用到数据库，
`sessions` 的列为 `id, session_id, user_id, created_at, expires_at`——`refresh_id` 已消失。

新增与改写的用例全部通过：

| 用例 | 覆盖 |
| --- | --- |
| `TestBearerSessionLifecycle` | 单一不透明 token 的签发、摘要落库、重复登录撤销旧 token、登出删除行 |
| `TestBearerTokenIgnoresCookies` | 任何名字的 Cookie 都不得充当凭证 |
| `TestMigrationRetiresTheRefreshColumn` | 在含 `refresh_id` 的库上应用 0004：旧会话清空、列被删除、重复执行安全 |
| `TestProtectedEndpointsRequireAValidSession` | 新增「只带 Cookie」矩阵，六个受保护接口一律 401 |
| `TestLoginSucceedsForSeededAccounts` / `TestLogoutRevokesTheSession` | 登录与注销响应均不含 `Set-Cookie` |

## HTTP 验收（经 Nginx）

28 项全部 PASS，含：

- `AU07 PASS` Cookie 不参与鉴权，链路不设置任何 Cookie——同一 token 值以 Cookie 形式重放返回 401，`/api/me` 响应不含 `Set-Cookie`。
- `AC01`/`AC02`/`AC29` 种子账号登录返回 Bearer token 与身份。
- `AC19` 登出后旧 token 失效。
- `AC05`/`AC10`/`AC11`/`AC12`/`AC15` 角色与班级隔离。
- `AC25`–`AC28` PDF/DOCX 解析与访问控制。
- `AC31` 登录限流统一返回 JSON 429。

## 浏览器端到端

| 场景 | 结果 |
| --- | --- |
| WE02 未登录进入登录页；错误密码留在登录页；正确凭证进入材料页 | PASS |
| WE01 刷新后由 GET /api/me 恢复登录，且浏览器 cookie 数为 0、`document.cookie` 为空 | PASS |
| WE03 教师上传、列表刷新、详情纯文本、下载走带 Bearer 头的请求 | PASS |
| WE04 学生无上传 UI，localStorage 篡改不改变身份，材料内脚本不执行 | PASS |
| WE05 登出后回到登录页，刷新仍未登录 | PASS |
| WE06 登录 429 显示通用稍后重试提示且保持未登录 | PASS |
| AC23 浏览器只访问 `localhost:8080` 这一个来源 | PASS |

## 环境说明

本机 Playwright 缓存中的 Chromium（`chromium-1217`）网络服务异常：启动即报
`Failed to load …resources.pak`、`Sandbox cannot access executable …(0x5)` 与
`Network service crashed or was terminated`，导致页面无法加载、导航后文档为空。
这与 `adopt-editorial-ui-style-2026-09-29.md` 记录的是同一类本机问题。

按 README 5.6 的既有做法，将 `CHROME_PATH` 指向本机 Chrome 后运行，原脚本与断言
完整通过，未放宽任何断言。

## 本次未覆盖

- `TestChunkFulltextIndexUsesTheNgramParser`（属 `add-traceable-vector-retrieval`）仍失败：
  它断言 `SHOW CREATE TABLE` 含 `WITH PARSER ngram`，而 MySQL 实际渲染为
  ``WITH PARSER `ngram` ``（带反引号）。索引定义本身正确，是断言写法问题，按用户决定未修改。
- `backend/migrations/0005_knowledge_chunks.sql` 的 `separator` 列名是 MySQL 保留字，
  已加反引号修复（列定义与 CHECK 各一处），否则该迁移无法应用、整个集成套件无法运行。
