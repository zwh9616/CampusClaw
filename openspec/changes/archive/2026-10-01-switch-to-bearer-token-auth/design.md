## Context

参见 proposal.md。当前 Go 服务生成 32 字节随机 token，以其 SHA-256 摘要保存于 sessions.session_id。本变更规划时曾保留一条 HttpOnly 刷新 Cookie 与 `/api/auth/refresh`，以便页面刷新后恢复登录；老师的要求是浏览器侧不出现 Cookie，方案据此收窄：只保留一种凭证，前端把它存进 localStorage，完全删除 Cookie 与刷新接口。已按旧方案实施的服务端刷新链路与前端重试逻辑需要回退。

## Goals / Non-Goals

**Goals:** 业务 API 仅凭 Bearer 访问 token 鉴权；浏览器链路中不出现任何 Cookie；页面刷新后由前端自行恢复登录，直到 24 小时会话到期；角色和班级仍由服务端回表确定。
**Non-Goals:** 不采用 JWT、OAuth 授权服务器、跨域 API、独立的刷新凭证或永久登录；不改变材料数据或检索业务规则。

## Decisions

1. **单一不透明凭证。** 登录生成 256 位随机 token，以其 SHA-256 摘要存入 sessions.session_id，原值只出现在登录响应的 JSON 中。会话固定 24 小时，到期即 401，用户重新登录。相比 JWT，数据库能立即撤销凭证，且班级/角色始终从 users 回表读取。旧方案曾并行发放独立的刷新凭证并新增 sessions.refresh_id，本次一并移除。

2. **完全无 Cookie。** 登录、注销响应 MUST NOT 设置任何 Cookie；鉴权 MUST NOT 读取任何 Cookie。RequireUser 严格解析单个 Authorization: Bearer 请求头，拒绝 Cookie、query/body token、重复或格式错误的头。删除 `/api/auth/refresh` 路由及其 handler。POST /api/logout 以访问 token 删除 Session 行并返回 204。

3. **迁移回退 refresh_id 并清空既有会话。** 迁移 `0003_bearer_refresh.sql` 已在本机数据库应用，且 sessions.refresh_id 是 NOT NULL 且无默认值的唯一列——若只改代码而不处理该列，所有插入都会失败。迁移工具按文件名记账（`backend/migrations/migrate.go`），改写一个已应用的脚本不会重跑，因此必须新增 `0004_drop_refresh_id.sql`：先 `DELETE FROM sessions`，再 `DROP COLUMN refresh_id`。旧会话因此失效一次，用户重新登录。

4. **前端把访问 token 保存在 localStorage。** `api.ts` 的统一请求层从 `localStorage` 读取 `campusclaw.access_token` 并附加 Bearer 头。应用启动时若存在该键，调用 GET /api/me 校验：200 恢复材料页，401 清除该键并进入登录页，网络/5xx 显示可重试错误且不得误判为登出。任何受保护请求收到 401 即清除该键。登录成功后写入，注销成功后删除；注销失败（网络/5xx）不得声称成功。MUST NOT 把 token 放入 URL、DOM 或日志。

5. **下载与注销。** 原有直接下载链接无法带 Authorization，改用带 Bearer 头的 fetch 获取 Blob，再使用临时 object URL 保存服务器给出的安全附件名并释放 URL。材料班级授权与错误码不变。

6. **代理与后续检索接口。** Nginx/Vite 继续代理同源 `/api` 并透传 Authorization，链路中不再有 Set-Cookie。PUBLIC_ORIGIN 仍约束 POST 写请求，DEV_PUBLIC_ORIGIN 的本地开发组合与启动校验保持不变。正在规划的检索接口沿用 RequireUser。

## Risks / Trade-offs

- **token 存 localStorage 可被 XSS 读取** → 这是相对 HttpOnly Cookie 方案的净安全下降，属于老师指定方案下的已知取舍。缓解：材料正文始终按纯文本渲染、不执行 HTML/脚本；token 不写入 URL、DOM 或日志；生产链路强制 HTTPS；会话固定 24 小时，泄露窗口有界。
- **浏览器可能残留旧 Cookie** → 服务端不读取任何 Cookie，残留值不参与鉴权；`0004` 清空会话行使旧 cookie 值即使被回放也不再对应有效会话。
- **多标签页各自持有同一 token** → 无轮换，不存在互踢；注销后其他标签页下次请求收到 401 并回到登录页。
- **下载 Blob 占用内存** → 现有原文件上限 5 MiB，可接受；完成后释放对象 URL。

## Migration Plan

1. 回退刷新链路：删除 `/api/auth/refresh`、`store.Refresh`、刷新 Cookie 辅助函数与 `Authenticator.secure`；`Credentials` 收成 `{Access, ExpiresAt}`；新增 `0004_drop_refresh_id.sql`。保留并核对 Bearer 中间件与登录 JSON。
2. 前端改为 localStorage 存储与启动回读校验，删除刷新重试逻辑；调整 Nginx/Vite 与配置示例。
3. 同步发布 web/api，验收刷新前后身份、无 Set-Cookie、注销撤销、跨班隔离、下载字节、同源限制、登录限流和代理头透传。回退时同步回退 web/api。
