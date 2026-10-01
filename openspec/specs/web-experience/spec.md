# web-experience Specification

## Purpose
定义登录、会话恢复、材料页面与统一视觉体验的用户可见行为，使师生在同源应用中安全访问本班教学材料，并在桌面和窄屏设备上清楚地完成操作。

## Requirements

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

### Requirement: WEB-04 Class-scoped search and source display

登录的教师和学生 MUST 能从材料页面输入自然语言查询并选择 keyword、vector、hybrid，默认 hybrid；页面 MUST 显示检索状态、按次序排列的本班命中、材料标题、切片序号、字符区间、纯文本摘录和打开原材料的入口。无命中 MUST 显示「资料中未找到相关内容」，不能把空结果当作网络错误。401 MUST 清理内存身份并回到登录页；400 MUST 提示修改输入，503 MUST 显示可重试状态。检索页面 MUST 使用同源 /api、沿用 WEB-03 的视觉语言，并在桌面和 390px 窄屏可用、无横向滚动且有可见键盘焦点。MUST NOT 在页面中展示原始向量分量或网关密钥。

#### Scenario: WE10 Search and inspect citation
- **GIVEN** Student A1 已登录且本班有 ready 切片
- **WHEN** 分别选择三种模式查询，并打开一条命中的来源材料
- **THEN** 页面显示对应模式、材料标题、切片序号、字符区间和安全文本摘录，点击来源沿用本班授权详情；桌面与 390px 视口的操作均可用

#### Scenario: WE11 Empty, invalid and unavailable search
- **GIVEN** 用户处于检索页面
- **WHEN** 依次提交无依据问题、空输入，并在向量服务不可用时选择 vector
- **THEN** 依次显示固定无依据提示、输入校验提示和可重试故障提示，不显示虚构出处或原始向量

### Requirement: WEB-05 Evidence-backed answer and teacher index controls

页面 MUST 提供简短问答入口，将回答中的 [1]、[2] 等标记与同序出处列表关联；没有引用时 MUST 显示固定无依据文案和空出处。教师 MUST 能为上传选择切分策略，并对本班材料显式发起重建索引、查看 pending/ready/failed 状态及失败后的重试入口；学生 MUST NOT 看到重建控制，但仍可检索本班 ready 内容。前端隐藏控制 MUST NOT 代替服务端授权。回答、出处及索引状态在桌面和 390px 视口 MUST 清晰可用。

#### Scenario: WE12 Ask with aligned citations
- **GIVEN** 本班有支持答案的两条切片
- **WHEN** 学生提问并选择回答中的 [2]
- **THEN** 页面中 [2] 对应第二条出处，可查看材料标题、切片位置和授权材料详情；无依据时答案为固定文案且没有出处

#### Scenario: WE13 Teacher rebuild and student restriction
- **GIVEN** 教师与学生分别登录同一班级
- **WHEN** 教师选择 hierarchy 重建本班材料，学生随后浏览材料与检索页面
- **THEN** 教师可见索引进度及失败重试；学生无重建入口，只看可用的 ready 命中；伪造页面角色不改变服务端权限
