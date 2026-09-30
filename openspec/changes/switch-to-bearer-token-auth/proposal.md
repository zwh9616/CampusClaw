## Why

老师希望将登录鉴权改为 token 方式；本变更按常见的 Authorization: Bearer <token> 请求头方案规划。现有实现虽然已经生成随机会话 token，却只通过 HttpOnly Cookie 传递，前端无法按 `Authorization: Bearer <token>` 调用接口。方案进一步要求浏览器侧不出现 Cookie：登录只下发一个不透明 token，由前端自行保存与携带。

## What Changes

- **BREAKING**：登录成功后在 JSON 中返回不透明访问 token 和到期时间，MUST NOT 设置任何 Cookie；受保护业务接口只接受 Bearer 请求头，不接受 Cookie、URL 参数或请求体中的访问 token。
- 复用现有 256 位随机 token、服务端 `sessions` 摘要存储、24 小时有效期、角色与班级回表校验及注销撤销；不引入 JWT，也不引入独立的刷新凭证或刷新接口。
- 前端把访问 token 保存在 `localStorage`，统一为受保护请求附加 Bearer 头；页面刷新后从 `localStorage` 读回 token 并调用 `GET /api/me` 校验身份，在 24 小时会话到期前保持登录。401 清除本地凭证并回到登录页，网络/5xx 保留可重试状态而不是误判为登出。
- 原文件下载改为带 Bearer 头的请求，再由浏览器保存响应文件；保持授权、文件名和原始字节行为。
- 更新本地开发代理和认证相关规格、测试与文档；旧 Session Cookie 不再参与任何鉴权，回退刷新凭证字段的迁移会清空既有会话行，部署切换后需重新登录一次。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `identity-session`: 登录返回不透明 Bearer token 且不设置任何 Cookie，受保护接口只从 Authorization 头恢复身份，注销删除服务端 Session。
- `web-experience`: 前端在 localStorage 保存访问 token、页面刷新后回读并校验身份，并通过带认证头的请求下载文件。
- `compose-runtime`: 本地代理转发 Authorization 头，链路中不再出现任何 Cookie。
- `class-data`: Session 只保存单一 token 摘要，新增回退 refresh_id 的迁移，种子账号登录验收改为检查 Bearer 响应、无 Set-Cookie 与 /api/me。

## Impact

涉及 Go `auth` 的登录/中间件/注销、前端 `api.ts` 与页面状态、下载入口、相关集成测试、Nginx/Vite 代理验证、环境配置与一条新迁移。材料、检索及问答接口继续使用同一服务端身份与班级边界；正在规划的 `add-traceable-vector-retrieval` 在实施时也须走统一 Bearer 中间件。

本变更的规划产物曾按保留刷新 Cookie 的旧方案部分实施；本次修订收窄为纯 token 方案，已实施的服务端刷新链路与前端刷新重试逻辑需按 tasks.md 回退。
