## Purpose

为教师和学生提供通过同源浏览器使用的登录与教学材料页面，启动时从服务器恢复身份，按真实角色呈现上传入口，并提供本班材料查看、下载和登出反馈，避免将客户端状态误作授权依据。

## ADDED Requirements

### Requirement: WEB-01 Login and session restoration

React 18 + TypeScript + Vite 前端 MUST 提供 Login Page 和 Materials Page，启动时 MUST GET /api/me；200 恢复登录，401 进入登录页，网络/5xx MUST 显示可重试错误。MUST 使用同源相对 /api 地址；本地 Vite 开发时仍由浏览器请求当前 5173 来源的 /api/*，经 RUN-05 代理访问后端，MUST NOT 直接访问 API 容器或以 localStorage 身份声明进行授权。登录 429 MUST 显示通用的稍后重试提示，MUST NOT 将其显示成密码错误或自动反复重试。

#### Scenario: WE01 Restore authenticated user
- **GIVEN** 浏览器有有效 Session
- **WHEN** 打开/刷新应用
- **THEN** me 200 后显示材料页面及当前 username、role、class_name/class_id

#### Scenario: WE02 Login and unauthenticated startup
- **GIVEN** 浏览器没有有效 Session
- **WHEN** 打开应用，再通过登录表单提交正确凭证
- **THEN** me 401 时显示登录页；登录成功后显示本班材料；错误密码则留在登录页并显示统一错误

#### Scenario: WE06 Login is rate limited
- **GIVEN** 用户在登录页，公网登录入口已触发 AUTH-06 限流
- **WHEN** 登录请求收到统一 JSON 429
- **THEN** 页面显示不涉及账号是否存在的稍后重试提示，不建立登录状态，不自动连续发送登录请求

### Requirement: WEB-02 Materials and role-specific controls

材料页 MUST 显示当前用户、角色、班级、列表、查看入口、下载入口和登出。teacher MUST 显示支持 .md/.txt/.pdf/.docx 的上传 UI，并说明 5 MiB 上限、PDF/DOCX 仅提取文本及不支持 OCR；解析失败 MUST 显示错误且不提示成功；student MUST NOT 显示；MUST NOT 以 UI 隐藏替代服务端 RBAC。上传 201 后 MUST 刷新列表；详情 MUST 以安全纯文本展示，MUST NOT 执行材料内 HTML/脚本。登出或受保护请求 401 MUST 清理内存身份并返回登录页。

#### Scenario: WE03 Teacher uploads and views
- **GIVEN** Teacher A 已登录
- **WHEN** 分别选择 .md/.txt/.pdf/.docx 并上传，随后打开详情和下载
- **THEN** 上传入口存在，成功后列表立即更新，原文或提取文本安全显示，下载通过 /api/materials/{id}/file

#### Scenario: WE04 Student interface and tampering
- **GIVEN** Student A1 已登录，材料内容包含 HTML/script 文本
- **WHEN** 查看页面，并修改 localStorage role 为 teacher 后刷新
- **THEN** 无上传 UI，身份仍为 student，可列出/查看/下载本班文件，脚本不执行

#### Scenario: WE05 Logout and errors
- **GIVEN** 用户处于材料页面
- **WHEN** 登出，或会话过期后请求材料
- **THEN** 清内存身份并进入登录页；普通 403/404 显示通用错误，网络/5xx 提供重试且不伪造身份

