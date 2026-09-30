## MODIFIED Requirements

### Requirement: WEB-01 Login and session restoration

React 18 + TypeScript + Vite 前端 MUST 提供 Login Page 和 Materials Page。登录成功后 MUST 把服务端返回的 Bearer token 保存在 localStorage 的 campusclaw.access_token 键，受保护的同源相对 /api 请求 MUST 附带 Authorization: Bearer <token>；MUST NOT 把 token 写入 sessionStorage、IndexedDB、Cookie、URL、DOM 或日志。打开或刷新应用时 MUST 从该键回读 token 并调用同源 GET /api/me 校验：200 以服务端返回的身份恢复材料页，401 清除该键并进入登录页，网络/5xx 显示可重试错误且不得误判为登出。任一受保护请求返回 401 MUST 清除该键并回到登录页，MUST NOT 自动重试，也 MUST NOT 请求任何刷新接口。身份与角色 MUST 只信任服务端。MUST 使用同源相对 /api 地址；本地 Vite 开发时仍由浏览器请求当前 5173 来源的 /api/*，经 RUN-05 代理访问后端，MUST NOT 直接访问 API 容器，也 MUST NOT 依据本地存储中的身份字段授权。登录 429 MUST 显示通用的稍后重试提示，MUST NOT 将其显示成密码错误或自动反复重试。

#### Scenario: WE01 Restore authenticated user
- **GIVEN** 用户已登录，token 已保存在 localStorage，当前页面可查看本班材料
- **WHEN** 刷新应用或关闭后重新打开
- **THEN** 页面从 localStorage 回读 token 并调用 GET /api/me，200 时恢复本班材料；本地存储中只有服务端签发的不透明 token，页面不依据其中的任何身份字段授权

#### Scenario: WE02 Login and unauthenticated startup
- **GIVEN** 新打开的浏览器页面 localStorage 中没有 token
- **WHEN** 打开应用，再通过登录表单提交正确凭证
- **THEN** 无 token 时不发起受保护请求并直接显示登录页；登录成功后在 localStorage 保存 token 并显示本班材料；错误密码则留在登录页并显示统一错误

#### Scenario: WE06 Login is rate limited
- **GIVEN** 用户在登录页，公网登录入口已触发 AUTH-06 限流
- **WHEN** 登录请求收到统一 JSON 429
- **THEN** 页面显示不涉及账号是否存在的稍后重试提示，不建立登录状态，不自动连续发送登录请求

#### Scenario: WE14 Transient API failure retains the stored token
- **GIVEN** 当前页面持有有效 token
- **WHEN** /api/me 或材料请求发生网络/5xx 故障，随后服务恢复
- **THEN** 页面提供重试且不清除 localStorage 中的 token，也不把故障误判为登出

### Requirement: WEB-02 Materials and role-specific controls

材料页 MUST 显示当前用户、角色、班级、列表、查看入口、下载入口和登出。teacher MUST 显示支持 .md/.txt/.pdf/.docx 的上传 UI，并说明 5 MiB 上限、PDF/DOCX 仅提取文本及不支持 OCR；解析失败 MUST 显示错误且不提示成功；student MUST NOT 显示；MUST NOT 以 UI 隐藏替代服务端 RBAC。上传 201 后 MUST 刷新列表；详情 MUST 以安全纯文本展示，MUST NOT 执行材料内 HTML/脚本。原文件下载 MUST 对 /api/materials/{id}/file 发起带 Bearer 头的请求，成功时保存与响应一致的原始字节并采用服务端提供的安全附件名；MUST NOT 把 token 放入下载 URL 或使用不带认证头的直接链接。成功登出或认证 401 MUST 清除 localStorage 中的 token 与身份并返回登录页；403/404 和网络/5xx MUST 维持现有错误与重试行为。

#### Scenario: WE03 Teacher uploads and views
- **GIVEN** Teacher A 已登录并持有有效 Bearer token
- **WHEN** 分别选择 .md/.txt/.pdf/.docx 并上传，随后打开详情和下载
- **THEN** 上传入口存在，成功后列表立即更新，原文或提取文本安全显示，下载经带 Bearer 头的 /api/materials/{id}/file 完成且字节与原文件一致

#### Scenario: WE04 Student interface and tampering
- **GIVEN** Student A1 已登录，材料内容包含 HTML/script 文本
- **WHEN** 查看页面，并把 localStorage 中的身份字段改成 teacher 后刷新
- **THEN** 无上传 UI，刷新后仍从服务端取回 student 身份，可列出、查看和下载本班文件，脚本不执行

#### Scenario: WE05 Logout and errors
- **GIVEN** 用户处于材料页面
- **WHEN** 成功登出，或 token 已失效后请求材料
- **THEN** 清理 localStorage 中的 token 和身份并进入登录页；普通 403/404 显示通用错误，网络/5xx 提供重试且不伪造身份

#### Scenario: WE16 Failed logout remains retryable
- **GIVEN** 用户持有仍有效的 Bearer token
- **WHEN** 注销请求发生网络或 5xx 故障
- **THEN** 页面提示重试且不声称已注销，localStorage 中的 token 保留；只有 204 或认证 401 才清除本地登录状态

#### Scenario: WE15 File download needs Authorization header
- **GIVEN** Student A1 已登录，本班有可下载文件
- **WHEN** 在页面点击下载，并分别尝试移除 Bearer 头或改用带 token 的 URL
- **THEN** 页面发出的带头请求可下载正确字节与附件名；无头请求 401，页面不把 token 暴露在 URL、DOM 链接或日志中
