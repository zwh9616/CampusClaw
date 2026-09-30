## MODIFIED Requirements

### Requirement: AUTH-01 Password login

系统 MUST 仅支持 teacher/student，通过 POST /api/login 接受 JSON username、password，以 bcrypt 校验密码，成功创建服务端 Session 并返回 200 {user:{id,username,role,class_id,class_name},token,expires_at}；token MUST 为本次新建 Session 的不透明原始值，expires_at MUST 为明确时区的 UTC 时间。成功响应 MUST NOT 设置任何 Cookie，并 MUST 设置 Cache-Control: no-store。错误密码和未知用户名 MUST 返回相同 401 错误体；MUST NOT 保存明文密码或在源码、SQL、前端、Dockerfile、Git 配置中硬编码真实秘密。缺字段/非法 JSON MUST 返回 400；超过 72 字节的密码 MUST NOT 截断，登录返回统一 401。

#### Scenario: AC01 Teacher login
- **GIVEN** seed 中 Teacher A 的用户名 teacher_a 与环境变量密码
- **WHEN** POST /api/login 提交正确凭证
- **THEN** 返回 200，user.role=teacher、班级为 Class A，JSON 中包含访问 token 和 expires_at，且响应头不含任何 Set-Cookie

#### Scenario: AC02 Student login
- **GIVEN** seed 中 Student A1 的用户名 student_a1 与环境变量密码
- **WHEN** POST /api/login 提交正确凭证
- **THEN** 返回 200，user.role=student、班级为 Class A，JSON 中包含访问 token 和 expires_at，且响应头不含任何 Set-Cookie

#### Scenario: AC03 Wrong credentials
- **GIVEN** 已存在用户以及一个不存在的用户名
- **WHEN** 分别提交已有用户的错误密码和未知用户凭证
- **THEN** 两者均返回 401 且响应体一致，不返回 token、不设置任何 Cookie、不创建有效 Session，不暴露用户名是否存在

### Requirement: AUTH-02 Server-side identity and session lifecycle

Bearer token MUST 为安全随机、不可预测的不透明字符串，具有至少 256 位随机性，MUST NOT 编码可信 user_id、role 或 class_id。客户端 MUST 用单一 Authorization: Bearer <token> 请求头访问受保护业务接口；服务端 MUST 只从该请求头读取访问 token，MUST NOT 读取或接受任何 Cookie、URL 查询参数或请求体中的 token，缺失、重复、格式错误、过期、撤销或未知 token MUST 返回 401。认证失败响应 MUST 带 WWW-Authenticate: Bearer，且不泄露 token。服务端 MUST 对原始访问 token 的 ASCII 字节计算 SHA-256 摘要，以 64 字符小写十六进制摘要查询 sessions.session_id，再关联 users 恢复当前用户；数据库 MUST NOT 保存原始 token。成功登录 MUST 创建新的随机 Session；若请求携带有效的旧 Bearer token，MUST 撤销该旧 Session；无旧 token 时仍可正常登录。Session 固定有效 24 小时；MUST NOT 提供独立的刷新凭证，也 MUST NOT 通过任何接口延长 expires_at。生产链路 MUST 使用 HTTPS 传输 Bearer token，本机 localhost 教学环境可使用 HTTP。GET /api/me MUST 返回 200 的 id、username、role、class_id、class_name；身份数据 MUST 来自服务端，MUST NOT 信任任何客户端提交的身份字段。浏览器 MAY 把服务端签发的不透明 token 保存在本地存储中并在刷新后回读，但该 token MUST NOT 自身携带或声明身份，每次请求 MUST 仍按上述摘要回表校验。

#### Scenario: AC04 Protected APIs require authentication
- **GIVEN** 无 Authorization、伪造 token、已过期 token、重复或格式错误的 Authorization、以及只带 Cookie 不带 Authorization（逐一测试）
- **WHEN** 请求 GET /api/me、GET /api/materials、GET /api/materials/{id}、GET /api/materials/{id}/file、POST /api/materials、POST /api/logout（逐一测试）
- **THEN** 各请求返回 401 和 Bearer 挑战，不查询或写入可访问的班级业务数据，上传不留下文件

#### Scenario: AU01 Identity tampering and cookie attributes
- **GIVEN** Student A1 已持有有效 Bearer token
- **WHEN** 在 query/body/Cookie/localStorage 中加入 role=teacher 与 Class B 的 class_id 后查询 /api/me 并尝试上传
- **THEN** me 仍返回数据库中的 student/Class A，上传 403；仅携带 Cookie 而不带 Authorization 的请求返回 401，Cookie 不参与鉴权

#### Scenario: AU02 Session rotation and expiry
- **GIVEN** 客户端持有有效 Bearer token
- **WHEN** 携带它再次正确登录，然后分别用旧 token、过期的新 token 请求 /api/me
- **THEN** 登录产生不同随机 token，旧 token 和过期 token 均返回 401，删除过期行的任务是否已运行不影响结果

#### Scenario: AU06 Session digest storage
- **GIVEN** 用户成功登录并从 JSON 取得原始 token
- **WHEN** 对 token 的 ASCII 字节计算 SHA-256 并编码为小写十六进制，检查数据库 Session 行，然后携带 Authorization: Bearer <token> 请求 me
- **THEN** sessions.session_id 恰等于该摘要且与原 token 不同，数据库未保存原 token，摘要唯一约束有效，me 返回 200

### Requirement: AUTH-03 Logout revokes server session

POST /api/logout MUST 以 Authorization 请求头中的 Bearer 访问 token 删除对应的服务端 Session，成功返回 204；无效或缺失访问 token MUST 返回 401。客户端在登出成功或收到 401 时 MUST 清除本地保存的 token。服务端 MUST NOT 仅要求客户端忘记 token 而保留 Session 有效性。登出响应 MUST NOT 设置 Cookie，MUST NOT 依据任何 Cookie 注销 Session。

#### Scenario: AC19 Logout invalidation
- **GIVEN** 已登录且保存了当前 token
- **WHEN** 携带 Bearer token 调用 POST /api/logout 后再次携带保存的 token 请求 GET /api/me
- **THEN** 登出返回 204 且不设置 Cookie，该 token 随即失效，me 返回 401

#### Scenario: AU07 Cookie cannot authorize logout or any protected request
- **GIVEN** 浏览器携带任意 Cookie，但没有 Authorization 请求头
- **WHEN** 请求 POST /api/logout 或任一受保护业务接口
- **THEN** 返回 401，Cookie 不能代替 Bearer 访问 token 授权

### Requirement: AUTH-05 Same-origin mutations and private responses

对 POST 请求，若存在 Origin，MUST 精确匹配 PUBLIC_ORIGIN 或满足以下明确启用的本地开发来源，MUST NOT 要求同时存在 Referer；若无 Origin 但存在 Referer，MUST 验证其 scheme、host、有效 port 与当前允许的来源之一同源。Sec-Fetch-Site=cross-site MUST 拒绝，不因其他头匹配而放行。若三者均缺失，MUST 允许非浏览器 API 测试客户端继续认证后的流程；若仅存在 Sec-Fetch-Site，MUST 仅接受 same-origin 或 none，其他值返回 403。存在但为空/格式非法的 Origin 或 Referer MUST 返回 403；Origin 优先且不得在 Origin 校验失败后回退到 Referer。受保护 POST 的认证与角色检查 MUST 先于来源校验；认证及受保护数据 MUST no-store，MUST NOT 记录密码、Bearer token、Authorization 头或 DSN。

仅当 PUBLIC_ORIGIN=http://localhost:8080 时，服务端 MAY 接受显式配置的 DEV_PUBLIC_ORIGIN=http://localhost:5173，供本机 Vite 开发代理使用；空值视为未启用。DEV_PUBLIC_ORIGIN 非空但不满足上述固定本地组合时 MUST 拒绝启动。生产部署 MUST 不启用该开发来源；MUST NOT 依据请求 Host 或客户端可控的 X-Forwarded-* 扩大来源白名单。两种允许来源共用上述 Origin/Referer/Sec-Fetch-Site 校验规则，不开放 CORS。客户端后续受保护业务请求中的 Bearer token MUST 仅通过 Authorization 请求头传递，MUST NOT 出现在请求 URL、请求体或日志中。

#### Scenario: AU03 Cross-origin upload rejected
- **GIVEN** Teacher A 的有效 Bearer token
- **WHEN** 携带外站 Origin 向上传 API 提交合法文件
- **THEN** 返回 403，文件和数据库无变更；同源 Origin 的相同请求可成功

#### Scenario: AU04 Origin and Referer precedence
- **GIVEN** Teacher A 有效 Bearer token 与合法文件，各案例独立执行
- **WHEN** 分别仅带同源 Origin、仅带同源 Referer、三者全无、仅 Sec-Fetch-Site=same-origin 或 none
- **THEN** 均可继续并成功 201，不要求 Origin 与 Referer 同时存在

#### Scenario: AU05 Source validation failures
- **GIVEN** Teacher A 有效 Bearer token 与合法文件
- **WHEN** 分别提交外站 Origin 加同源 Referer、只有外站 Referer、Origin=null、空 Origin、同源 Origin 加 Sec-Fetch-Site=cross-site、仅 Sec-Fetch-Site=same-site
- **THEN** 均 403 且无材料、知识条目和原文件写入
