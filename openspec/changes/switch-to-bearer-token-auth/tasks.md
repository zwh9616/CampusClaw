## 1. 回退刷新链路，回到单一 Bearer 凭证

- [ ] 1.1 移除刷新接口及其存储层：删除 `POST /api/auth/refresh` 路由、`Handlers.Refresh`、`Store.Refresh`，以及 `session_token.go` 中的 `RefreshCookieName`/`RefreshCookiePath`/`RefreshToken`/`SetRefreshCookie`/`ClearRefreshCookie` 和 `Authenticator` 的 `secure` 字段与 `Secure()` 方法；`Store.Credentials` 收成 `{Access, ExpiresAt}`，`Issue` 只生成一个 token。用单元测试核对登录、鉴权、注销路径不再引用 Cookie 或刷新凭证。
- [ ] 1.2 新增迁移 `0004_drop_refresh_id.sql`：先 `DELETE FROM sessions;`，再 `ALTER TABLE sessions DROP COLUMN refresh_id;`。用迁移测试核对在含 refresh_id 的库上插入恢复成功、旧会话行被清空、重复执行安全；同时确认不改写已应用的 `0003`。
- [ ] 1.3 登录与注销不再设置任何 Cookie：`Login` 返回 `{user,token,expires_at}` 且响应头无 Set-Cookie，`Logout` 以 Bearer 删除 Session 行并返回 204。用测试断言两个响应都不含 Set-Cookie，且只带 Cookie 的请求返回 401。

## 2. 前端改为 localStorage 存储与回读

- [ ] 2.1 在 api.ts 以 localStorage 的 `campusclaw.access_token` 管理访问 token，为业务请求附 Bearer 头，删除刷新调用与 401 后的刷新重试逻辑。用请求层测试验证登录写入、401 清除、网络/5xx 保留、token 不进入 URL/DOM/日志及无循环重试。
- [ ] 2.2 调整 App、登录与材料页状态；启动时仅在存在 token 时调用 GET /api/me，401 进入登录页，网络/5xx 提供重试而不是误判登出。用页面测试验证刷新后保持教师/学生登录、无 token 直接显示登录页、429 提示和注销失败不误报成功。
- [ ] 2.3 将材料下载改为带 Bearer 头的 fetch 与临时 Blob URL；用浏览器测试逐一核对 .md/.txt/.pdf/.docx 原始字节、附件名、错误处理、URL 清理和跨班 404。

## 3. 代理与整体验收

- [ ] 3.1 更新 Nginx/Vite 开发配置和环境示例，去掉 Cookie 相关配置；在 8080 与 5173 两个入口测试 Authorization 透传、链路中不出现任何 Cookie、同源限制、登录限流及日志不含凭证。
- [ ] 3.2 更新后端测试夹具及端到端测试，检查在途检索变更接入统一 Bearer 中间件的契约；运行 Go 测试、前端构建、浏览器流程和 `openspec validate --strict`，记录无 Set-Cookie、注销撤销、旧会话失效及同步回退 web/api 的验收结果。
