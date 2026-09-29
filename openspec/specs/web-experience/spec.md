# web-experience Specification

## Purpose
定义登录、会话恢复、材料页面与统一视觉体验的用户可见行为，使师生在同源应用中安全访问本班教学材料，并在桌面和窄屏设备上清楚地完成操作。

## Requirements

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

### Requirement: WEB-03 Consistent editorial interface

登录页和材料页 MUST 采用统一的「书卷文雅」视觉语言：以米白背景和纸张色内容面为基础，以北大红 `#94070A` 表达品牌和主要操作，以清晰易读的正文及克制的衬线标题组织信息。后续新增的前端页面 MUST 延续同一视觉语言；错误和成功状态 MUST 与品牌强调色区分。界面 MUST 在桌面和 390px 宽的窄屏视口下保持内容与操作可读、可用，且不得产生横向页面滚动。键盘操作时 MUST 有可见焦点。

#### Scenario: WE07 Login appearance and narrow layout

- **GIVEN** 访客尚未登录
- **WHEN** 分别在桌面和 390px 宽视口打开登录页
- **THEN** 显示统一品牌、账号表单与北大红主要按钮；窄屏下内容按单栏排列，输入和按钮可用，页面不产生横向滚动

#### Scenario: WE08 Materials appearance for both roles

- **GIVEN** 教师或学生已登录
- **WHEN** 分别在桌面和 390px 宽视口打开材料页
- **THEN** 用户信息、材料列表与可用操作沿用登录页的视觉语言；教师的上传区在窄屏下与列表按单栏排列，学生仍无上传入口，页面不产生横向滚动

#### Scenario: WE09 Focus and status distinction

- **GIVEN** 用户使用键盘操作登录页或材料页
- **WHEN** 焦点进入可交互控件，或页面显示错误、成功状态
- **THEN** 焦点清楚可见；错误与成功状态可辨识，且不依赖北大红品牌强调色表达状态
