## Purpose

为 CampusClaw 的教师和学生提供可撤销的服务端身份认证，确保登录、当前用户恢复、登出和上传角色判断均以数据库恢复的真实用户为依据，不接受客户端伪造身份或班级范围。

## ADDED Requirements

### Requirement: AUTH-01 Password login

系统 MUST 仅支持 teacher/student，通过 POST /api/login 接受 JSON username、password，以 bcrypt 校验密码，成功创建服务端 Session 并返回 200 {user:{id,username,role,class_id,class_name}}。错误密码和未知用户名 MUST 返回相同 401 错误体；MUST NOT 保存明文密码或在源码、SQL、前端、Dockerfile、Git 配置中硬编码真实秘密。缺字段/非法 JSON MUST 返回 400；超过 72 字节的密码 MUST NOT 截断，登录返回统一 401。

#### Scenario: AC01 Teacher login
- **GIVEN** seed 中 Teacher A 的用户名 teacher_a 与环境变量密码
- **WHEN** POST /api/login 提交正确凭证
- **THEN** 返回 200，user.role=teacher、班级为 Class A，并设置 Session Cookie

#### Scenario: AC02 Student login
- **GIVEN** seed 中 Student A1 的用户名 student_a1 与环境变量密码
- **WHEN** POST /api/login 提交正确凭证
- **THEN** 返回 200，user.role=student、班级为 Class A，并设置 Session Cookie

#### Scenario: AC03 Wrong credentials
- **GIVEN** 已存在用户以及一个不存在的用户名
- **WHEN** 分别提交已有用户的错误密码和未知用户凭证
- **THEN** 两者均返回 401 且响应体一致，不创建有效 Session，不暴露用户名是否存在

### Requirement: AUTH-02 Server-side identity and session lifecycle

Cookie MUST 仅含安全随机的不可预测 token（至少 256 位随机性），MUST NOT 包含可信 user_id、role 或 class_id。服务端 MUST 对 Cookie Token 字符串的原始 ASCII 字节计算 SHA-256 摘要，以 64 字符小写十六进制摘要查询 sessions.session_id，再关联 users 恢复当前用户；数据库 MUST NOT 保存原始 Token。成功登录 MUST 创建新的随机 Session，并 MUST 撤销请求所携带的当前浏览器旧 Session；无有效旧 Session 时仍可正常登录。Session 固定有效 24 小时，过期/撤销/未知 token MUST 返回 401。Cookie MUST 为 HttpOnly、SameSite=Lax、Path=/；生产 HTTPS MUST 启用 Secure，本地 HTTP 可关闭。GET /api/me MUST 返回 200 的 id、username、role、class_id、class_name；身份数据 MUST 来自服务端。MUST NOT 接受客户端 signed-cookie 身份或 localStorage 授权。

#### Scenario: AC04 Protected APIs require authentication
- **GIVEN** 无 Cookie、伪造 token 或已过期 token（逐一测试）
- **WHEN** 请求 GET /api/me、GET /api/materials、GET /api/materials/{id}、GET /api/materials/{id}/file、POST /api/materials、POST /api/logout（逐一测试）
- **THEN** 各请求返回 401，未查询/写入可访问的班级业务数据，上传不留下文件

#### Scenario: AU01 Identity tampering and cookie attributes
- **GIVEN** Student A1 已登录
- **WHEN** 在 query/body/额外 Cookie/localStorage 中加入 role=teacher 与 Class B 的 class_id 后查询 /api/me 并尝试上传
- **THEN** me 仍返回数据库中的 student/Class A，上传 403；会话 Cookie 不含身份字段且具备规定属性；生产配置具备 Secure

#### Scenario: AU02 Session rotation and expiry
- **GIVEN** 同一浏览器已有有效 Session
- **WHEN** 再次正确登录，然后分别用旧 token、过期的新 token 请求 /api/me
- **THEN** 登录产生不同随机 token，旧 token 和过期 token 均返回 401，删除过期行的任务是否已运行不影响结果

#### Scenario: AU06 Session digest storage
- **GIVEN** 用户成功登录并从 Set-Cookie 取得原始 token
- **WHEN** 对 token 的 ASCII 字节计算 SHA-256 并编码为小写十六进制，检查数据库 Session 行，然后携带原 token 请求 me
- **THEN** sessions.session_id 恰等于该摘要且与原 token 不同，数据库未保存原 token，摘要唯一约束有效，me 返回 200

### Requirement: AUTH-03 Logout revokes server session

POST /api/logout MUST 删除当前服务端 Session 并以匹配 Path 的过期 Cookie 清理浏览器，成功返回 204。无效会话 MUST 返回 401 并清 Cookie。MUST NOT 仅清客户端 Cookie 而保留 Session 有效性。

#### Scenario: AC19 Logout invalidation
- **GIVEN** 已登录且保存了当前 token
- **WHEN** POST /api/logout 后再次携带保存的 token 请求 GET /api/me
- **THEN** 登出返回 204，Cookie 被清理，Session 已失效，me 返回 401

### Requirement: AUTH-04 Teacher-only upload

Go MUST 独立检查 POST /api/materials 的当前用户角色，仅 teacher 可继续；student MUST 返回 403 Forbidden，未认证 MUST 返回 401。MUST 在解析上传文件或写文件/数据库前完成认证和角色检查。前端隐藏按钮 MUST NOT 被用作安全控制。

#### Scenario: AC05 Student direct upload
- **GIVEN** Student A1 有效 Session
- **WHEN** 绕过 UI 直接 POST /api/materials，分别提交合法和非法 multipart
- **THEN** 均返回 403，materials、knowledge_entries 和文件数量保持不变

### Requirement: AUTH-05 Same-origin mutations and private responses

对 POST 请求，若存在 Origin，MUST 精确匹配 PUBLIC_ORIGIN 或满足以下明确启用的本地开发来源，MUST NOT 要求同时存在 Referer；若无 Origin 但存在 Referer，MUST 验证其 scheme、host、有效 port 与当前允许的来源之一同源。Sec-Fetch-Site=cross-site MUST 拒绝，不因其他头匹配而放行。若三者均缺失，MUST 允许非浏览器 API 测试客户端继续认证后的流程；若仅存在 Sec-Fetch-Site，MUST 仅接受 same-origin 或 none，其他值返回 403。存在但为空/格式非法的 Origin 或 Referer MUST 返回 403；Origin 优先且不得在 Origin 校验失败后回退到 Referer。受保护 POST 的认证与角色检查 MUST 先于来源校验；认证及受保护数据 MUST no-store，MUST NOT 记录密码、token、DSN。

仅当 PUBLIC_ORIGIN=http://localhost:8080 且 SESSION_COOKIE_SECURE=false 时，服务端 MAY 接受显式配置的 DEV_PUBLIC_ORIGIN=http://localhost:5173，供本机 Vite 开发代理使用；空值视为未启用。DEV_PUBLIC_ORIGIN 非空但不满足上述固定本地组合时 MUST 拒绝启动。生产部署 MUST 不启用该开发来源；MUST NOT 依据请求 Host 或客户端可控的 X-Forwarded-* 扩大来源白名单。两种允许来源共用上述 Origin/Referer/Sec-Fetch-Site 校验规则，不开放 CORS。

#### Scenario: AU03 Cross-origin upload rejected
- **GIVEN** Teacher A 的有效 Session
- **WHEN** 携带外站 Origin 向上传 API 提交合法文件
- **THEN** 返回 403，文件和数据库无变更；同源 Origin 的相同请求可成功

#### Scenario: AU04 Origin and Referer precedence
- **GIVEN** Teacher A 有效 Session 与合法文件，各案例独立执行
- **WHEN** 分别仅带同源 Origin、仅带同源 Referer、三者全无、仅 Sec-Fetch-Site=same-origin 或 none
- **THEN** 均可继续并成功 201，不要求 Origin 与 Referer 同时存在

#### Scenario: AU05 Source validation failures
- **GIVEN** Teacher A 有效 Session 与合法文件
- **WHEN** 分别提交外站 Origin 加同源 Referer、只有外站 Referer、Origin=null、空 Origin、同源 Origin 加 Sec-Fetch-Site=cross-site、仅 Sec-Fetch-Site=same-site
- **THEN** 均 403 且无材料、知识条目和原文件写入

### Requirement: AUTH-06 Public login throttling and safe failure audit

公网 Nginx MUST 对精确路径 /api/login 按直接连接的客户端 IP 施加共享限流：持续速率 10 次/分钟，额外突发 5 次，突发请求不排队延迟；MUST NOT 用客户端传入的 X-Forwarded-* 作为限流键。限流 MUST 同时计入成功和失败的请求，MUST NOT 按用户名设置不同阈值或暴露账号是否存在。超限 MUST 在转发到 Go 前返回 429，响应 MUST 为统一 JSON 错误体 {"error":{"code":"rate_limited","message":"请求过于频繁，请稍后重试"}}，设置 Cache-Control: no-store，不创建或变更 Session。未超限时错误密码与未知用户名仍 MUST 返回同一 401 响应体。Go MUST 仅在服务端记录不含敏感值的失败原因码，区分未知账号、错误密码、超长密码；Nginx MUST 记录限流拒绝事件，日志 MUST NOT 包含密码、Session Token 或请求体。

#### Scenario: AC31 Login limit applies uniformly
- **GIVEN** 经 Nginx 使用同一客户端 IP，且限流状态已清空
- **WHEN** 在短时间内对 /api/login 混合提交已存在账号、未知账号、正确与错误密码，超过允许的突发容量
- **THEN** 限额内的错误密码和未知账号均返回相同 401；超限请求不论用户名与密码均返回相同 JSON 429、无 Set-Cookie 和有效新 Session；其他 API 不受登录限流影响，日志只含安全原因码或拒绝事件
