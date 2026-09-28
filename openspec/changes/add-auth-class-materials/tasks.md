## 1. 项目骨架与配置（后续任务基础）

- [x] 1.1 创建 frontend 的 React 18 + TypeScript + Vite 项目与锁文件，固定 React 18，提供 build/typecheck 脚本；验证 npm ci、npm run typecheck、npm run build 通过（RUN-01、WEB-01）。
- [x] 1.2 创建 backend Go module、cmd/api、internal 与 migrations 目录，使用 net/http、database/sql、MySQL 驱动、bcrypt，提供统一 JSON 错误与 ID 字符串序列化；验证 go build ./... 与基础响应契约测试通过（design D1/D4）。
- [x] 1.3 实现服务端环境配置校验，提供空值 .env.example、.gitignore/.dockerignore，列出数据库、三账号密码、PUBLIC_ORIGIN、SESSION_COOKIE_SECURE；验证缺少秘密启动失败且日志/构建产物无秘密，.env 不被跟踪（DATA-02、DA02）。

## 2. MySQL 数据与 seed（依赖 1）

- [x] 2.1 添加 MySQL 8.0.16+ 有序迁移及启动迁移入口，建立 classes/users/sessions 字段、唯一约束、角色 CHECK、外键、索引及 UTC 时间；在真实 MySQL 验证重复用户名/非法角色/无班级 user 被拒绝，迁移可重跑（DATA-01、DA01）。
- [x] 2.2 添加 materials/knowledge_entries 迁移，包含 design D3 全部非空字段、class_id 索引、每材料唯一知识记录及同班复合外键；执行 AC16、AC17、DA01，确认 NULL/省略班级、跨班关联及重复知识记录都被数据库拒绝。
- [x] 2.3 实现两班三账号事务 seed、唯一冲突复用及不符预期账号时报错，仅新账号从环境密码生成 bcrypt cost=12 hash；验证 DA02 和重复 seed 计数/hash 不变，密码长度非法时安全失败（DATA-02）。
- [x] 2.4 建立隔离 MySQL 集成测试库/卷与运行时随机测试秘密；验证测试不依赖宿主机数据库端口、不使用 SQLite、不能清理正常业务卷（RUN-02、DATA-01）。

## 3. 认证、Session 与权限（依赖 2）

- [x] 3.1 实现 Session token 安全生成、SHA-256 小写十六进制摘要唯一存储（禁止原始 token 入库）、24 小时过期、JOIN users 恢复身份及过期行清理；测试 AU01/AU02/AU06 和未知/过期 token 401，数据库摘要恰等于对 Cookie token ASCII 字节的 SHA-256，确认角色/班级来自当前数据库而非 Cookie（AUTH-02）。
- [x] 3.2 实现 POST /api/login 的 bcrypt 校验、统一错误响应、dummy hash 比较、新 Session 替换旧 Session；执行 AC01、AC02、AC03，并验证坏 JSON 400、超长密码 401（AUTH-01）。
- [x] 3.3 实现 HttpOnly/SameSite=Lax/Path=/ Cookie、生产 Secure 与 GET /api/me；执行 AU01、AC04，确认响应包含 id/username/role/class_id/class_name 且不返回 token（AUTH-02）。
- [x] 3.4 实现 POST /api/logout 删除服务端 Session 并清 Cookie，返回 204，无效 Session 返回 401 并清 Cookie；执行 AC19，用保存的旧 token 重放 me 确认 401（AUTH-03）。
- [x] 3.5 为全部受保护路由接入认证中间件，为 POST /api/materials 在解析 multipart 前执行 teacher 检查；执行 AC04、AC05，学生合法/非法上传均 403 且无文件或数据写入（AUTH-04）。
- [x] 3.6 实现来源检查、no-store 及敏感日志过滤，保持认证/角色校验顺序；执行 AU03–AU05，覆盖 Origin 优先、Referer 回退、三头全无允许及 Fetch-Site 分支，同源成功、外站 403、未登录 401、学生 403，检查日志不含测试秘密（AUTH-05）。

## 4. 班级隔离查询（依赖 2、3）

- [x] 4.1 实现必须传入服务端 class_id 的材料仓储，列表限定 class_id，详情限定 id+class_id，知识正文限定 material_id+class_id；使用跨班数据库 fixtures 验证 AC10/AC11，无无租户 handler 查询入口（MAT-01）。
- [x] 4.2 实现 GET /api/materials 的固定排序与 {materials:[]} 响应，忽略伪造 class_id/role/上传者 query；测试空列表及 Student B1 查询伪造 Class A 参数仍不可见（AC10）。
- [x] 4.3 实现 GET /api/materials/{id} 的元数据/全文响应、统一不存在/跨班/无效 ID 404；比较错误体、响应类型，确认无 stored_filename 泄漏，本班 fixture 200；以 MA05 验证 ASCII 正整数/前导零及 BIGINT UNSIGNED 溢出规则（AC11、MAT-01）。

## 5. 上传、文档解析与知识库事务（依赖 2、3、4，按本节顺序完成）

- [x] 5.1 实现单文件 multipart、title、5 MiB 文件/6 MiB 请求限制和四格式后缀白名单；文本仅强制完整 UTF-8/非空/无 NUL，不以短魔数误判；执行 AC08、MA01、MA06，确认 .doc/.docm/.zip/.exe 415、坏文本 400、大小超限 413且无脏行（MAT-02）。
- [x] 5.2 实现原始 filename 路径校验、净化 basename、随机 UUID、班级私有目录与排他写入/安全打开；执行 MA02，含原始 multipart 路径穿越、Windows 绝对路径与同名不覆盖，验证目录外无写入（MAT-02、design D5）。
- [x] 5.3 实现 Go 直接调用 pdfinfo/pdftotext 的 PDF 解析适配，固定 UTF-8/LF、200 页、10 秒和输出 5 MiB 上限，不经 shell；用含中英文/扫描/加密/损坏 PDF fixtures 验证 AC25/AC27 的提取结果与 400/413/422 分类，超时终止进程并回收（MAT-05）。
- [x] 5.4 用 archive/zip + encoding/xml 实现 DOCX 包校验、内部关系定位主文档、Transitional/Strict 段落/表格文本提取；执行 AC26/AC27，验证有效包文字顺序、损坏/普通 ZIP 400、加密/仅图片正文 422，无外链访问或磁盘解压（MAT-05）。
- [x] 5.5 实现 POST /api/materials 四格式分派、Session 归属、原文件保存与提取成功后单事务插入材料/知识记录；执行 AC06/AC07/AC15/AC25/AC26，核对 201/Location、原文或提取文本、content_type 与班级（MAT-02/MAT-03/MAT-05）。
- [x] 5.6 实现解析失败清理及明确事务失败回滚/文件补偿，加入仅测试可用的 knowledge INSERT 失败注入；执行 AC18/AC27/MA08，独立连接验证无新增两表记录，正常文件系统中本次文件删除（MAT-03）。
- [x] 5.7 实现最小失败记录：文件删除失败仍报告失败，COMMIT 结果未知返回 500、保留原文件、输出不含秘密的人工核对线索；执行 MA03，无虚报成功或盲删；不实现自动核对、定时/启动孤立文件扫描（MAT-03）。
- [x] 5.8 串联四格式真实上传与列表/详情，执行 AC09、MA04、AC28，确认 Student A1 立即可见预期文本，Student B1 列表不可见/详情 404，伪造归属不生效（MAT-01/MAT-05）。
- [x] 5.9 加入 DOCX entries/解压字节/XML 深度、输出/时间限制与解析并发槽位，禁止危险 part/DOCTYPE/外部主关系/宏；执行 MA07，验证 400/413/422/503、无外部请求/越界文件、两表无新增、临时资源释放（MAT-05）。

## 6. 下载（依赖 4、5）

- [x] 6.1 实现 GET /api/materials/{id}/file，认证及 id+class_id 查询后安全读取文件，设置 attachment、安全文件名、nosniff/no-store；对四格式执行 AC13/AC25/AC26，下载原文件字节校验一致（MAT-04）。
- [x] 6.2 覆盖跨班/不存在/无效 ID/文件丢失及未认证下载路径；执行 AC12/AC04/MA05/AC28，确认跨班与不存在均 404 同体、未登录 401，未授权不打开文件（MAT-04）。

## 7. Health、镜像、Nginx 与 Compose（依赖 1、2；完整验证依赖 3–6）

- [x] 7.1 实现无需认证的 GET /health，数据库 Ping 超时固定 2 秒，就绪 200、不可用 503 通用响应；测试 AC21/RU01，数据库恢复可自动恢复健康（RUN-04）。
- [x] 7.2 添加 Go 多阶段 Dockerfile、React 构建后复制 dist 的 Nginx Dockerfile，锁定工具链/镜像版本且不写秘密；在 Go API 运行镜像安装锁定版本 poppler-utils，启动检查 pdfinfo/pdftotext；分别构建并执行 RU02，检查运行镜像职责、非 root 用户与静态产物（RUN-01、design D1）。
- [x] 7.3 添加 Nginx SPA fallback、/api/ 原路径代理、精确 /health 代理、/uploads 及其子路径 404、请求大小限制、proxy_read_timeout=30s 并与 Go WriteTimeout 对齐；nginx -t 通过，验证 AC14、API 错误不回 HTML（RUN-02/RUN-03）。
- [x] 7.4 添加 compose.yaml 的 web/api/db、健康依赖、自动迁移/seed、db_data/uploads 持久化卷，仅 web ports=8080:80，web 无 uploads 挂载，API mem_limit=512m、cpus=1、pids_limit=64；验证 Compose 配置和 AC24，无 api/db 宿主机映射（RUN-01/RUN-02）。
- [x] 7.5 通过 Nginx 执行 AC21/AC22/AC24：配置秘密后 docker compose up --build 可启动并 health=200，全部业务 API 正常，不要求手工 SQL 或构建（RUN-01/RUN-04）。

## 8. React 页面（依赖 1、3–7）

- [x] 8.1 实现同源 API 客户端与启动 me 状态机、Login Page，401 清身份、网络/5xx 可重试；浏览器测试 WE01/WE02，刷新恢复与错误密码路径正确（WEB-01）。
- [x] 8.2 实现 Materials Page 的当前用户/角色/班级、列表、纯文本详情、下载入口；测试 WE03/WE04，HTML/script 不执行，下载只走认证 API（WEB-02）。
- [x] 8.3 实现 teacher 专属 title/file 上传 UI，accept=.md,.txt,.pdf,.docx，展示大小/文本提取/OCR 限制，201 后刷新，解析失败提示且不虚报成功；测试 teacher 可上传、student 无上传控件、localStorage 篡改不改变身份，Go 仍满足 AC05（WE03/WE04）。
- [x] 8.4 实现登出、401 返回登录、403/404 通用错误与网络重试；执行 WE05，登出后刷新仍未登录，旧 Session 重放失败（WEB-02、AC19）。

## 9. 集成验收与交付（依赖 1–8）

- [x] 9.1 编写 tests/acceptance 的 Cookie jar HTTP 测试入口，通过 Nginx 自动覆盖 AC01–AC15、AC19、AC21、AC25–AC28，运行输出每个 AC 编号的 pass/fail；核对状态码、响应体、下载字节及数据库计数（跨认证/材料/网关整体验收）。
- [x] 9.2 执行真实 MySQL 故障与约束集成套件，覆盖 AC16、AC17、AC18 及 DA01/DA02、MA03/MA08；验证失败注入仅测试可用、生产无故障 API，报告真实回滚结果。
- [x] 9.3 自动化完整持久化验收 AC20：上传后连续 seed/重启两次，比较 classes/users 数量、hash、材料/知识记录和文件字节；全部不变且原凭证可登录。
- [x] 9.4 执行浏览器端到端 WE01–WE05 与 AC23，记录请求 origin/port；全部业务请求仅 localhost:8080，检查学生 UI 与后端拒绝同时成立。
- [x] 9.5 在独立干净 Compose 项目中执行 AC22/AC24 和 AC14/AC21，保存启动、端口映射、health、静态绕过测试结果；不删除正常业务卷，确认 MySQL 无宿主机端口仍可工作。
- [x] 9.6 提交 README 的配置、seed 用户名、启动、测试入口、备份/回退、生产 Secure、文档解析支持范围/限额、失败状态码及未知提交文件人工核对原则说明；运行 go test ./...、MySQL 集成套件、npm run typecheck/build、浏览器/HTTP 验收入口，输出 AC01–AC28 及所有补充场景逐条结果，无跳过后才标记完成；核对没有 Non-goals 的表/API/任务。

## 10. 验收追踪索引（只作索引，不另建实现范围）

| 用户验收 | Spec 场景 | 实现/验证任务 |
| --- | --- | --- |
| 教师正确登录 | AC01 | 3.2、9.1 |
| 学生正确登录 | AC02 | 3.2、9.1 |
| 错密码 401 | AC03 | 3.2、9.1 |
| 未登录受保护 API 401 | AC04 | 3.3、3.5、6.2、9.1 |
| 学生直接上传 403 | AC05 | 3.5、8.3、9.1 |
| 上传 md | AC06 | 5.5、9.1 |
| 上传 txt | AC07 | 5.5、9.1 |
| 不支持类型无脏记录 | AC08 | 5.1、9.1 |
| 同班学生立即可见 | AC09 | 5.8、9.1 |
| B 班列表不见 A 班 | AC10 | 4.1、4.2、9.1 |
| B 班详情 404 | AC11 | 4.1、4.3、9.1 |
| B 班下载 404 | AC12 | 6.2、9.1 |
| A 班学生可下载 | AC13 | 6.1、9.1 |
| uploads 无法直取 | AC14 | 7.3、9.1、9.5 |
| 伪造班级不能写 B 班 | AC15 | 5.5、9.1 |
| materials 班级非空 | AC16 | 2.2、9.2 |
| knowledge 班级非空 | AC17 | 2.2、9.2 |
| knowledge 失败回滚 | AC18 | 5.6、9.2 |
| 登出旧 Session 失效 | AC19 | 3.4、8.4、9.1 |
| seed 幂等保留材料 | AC20 | 2.3、9.3 |
| health 200 | AC21 | 7.1、7.5、9.1、9.5 |
| Compose 一键启动 | AC22 | 7.5、9.5 |
| 浏览器仅 8080 | AC23 | 9.4 |
| MySQL 不映射宿主机 | AC24 | 7.4、7.5、9.5 |
| PDF 解析/上传/原文件下载 | AC25 | 5.3、5.5、6.1、9.1 |
| DOCX 段落/表格解析与下载 | AC26 | 5.4、5.5、6.1、9.1 |
| 损坏/加密/无可提取文本拒绝 | AC27 | 5.3、5.4、5.6、9.1 |
| 新格式继承全部访问控制 | AC28 | 5.8、6.2、9.1 |
| B 班教师登录 | AC29 | 11.1、11.2、11.3 |
| B 班教师上传与双向隔离 | AC30 | 11.2、11.3 |
| 登录入口按 IP 限流 | AC31 | 12.1、12.2、12.5 |
| Vite 开发同源代理 | AC32 | 12.3、12.4、12.5 |

补充场景映射：AU01/AU02/AU06 → 3.1–3.3；AU03–AU05 → 3.6；DA01/DA02 → 2.1–2.3、9.2；MA01/MA06 → 5.1；MA02 → 5.2；MA03 → 5.7、9.2；MA04 → 5.8；MA05 → 4.3、6.2；MA07 → 5.9；MA08 → 5.6、9.2；WE01–WE05 → 8.1–8.4、9.4；WE06 → 12.5；RU01 → 7.1；RU02 → 7.2。自动孤立文件恢复不在本次任务和验收门槛中。

## 11. B 班教师种子账号补充（新增工作；1–9 节勾选记录为此前完成状态）

- [x] 11.1 在服务端配置校验、Compose 环境传递及空值 .env.example 中加入必填的 SEED_TEACHER_B_PASSWORD；验证缺失或密码长度不合法时安全失败，仓库和日志均不含真实密码（DATA-02、DA02）。
- [x] 11.2 在事务 seed 中加入 teacher_b（teacher/Class B），仅新建账号时从 SEED_TEACHER_B_PASSWORD 生成 bcrypt hash；验证重复 seed 不重置四个账号密码、不改角色/班级、不清除 A/B 班材料，预存同名但角色或班级不符时报错（DATA-02、AC20）。
- [x] 11.3 扩展认证、材料与持久化验收：执行 AC29，确认 teacher_b 登录与 /api/me 的 Class B 身份；执行 AC30，确认 B 班教师上传后 Student B1 可列出/查看/下载、Student A1 无法访问；按四个种子账号重新执行 AC20 和 DA02，保存各场景结果。
- [x] 11.4 更新 README 的四个种子用户名与新增环境变量说明，并在新增验收完成后记录 AC29/AC30、AC20、DA02 的逐场景结果；全部完成后才勾选本节任务。

## 12. 登录限流与 Vite 本地代理（新增工作；此前勾选记录为已完成范围）

- [x] 12.1 在 Nginx 的 http 上下文按 $binary_remote_addr 建立共享限流区，仅对精确 /api/login 应用 10r/m、burst=5、nodelay；429 返回固定 JSON rate_limited、no-store、无 Set-Cookie，登录请求仍按原路径代理且不信任 X-Forwarded-* 作为限流键（AUTH-06、AC31）。
- [x] 12.2 为 Go 登录失败增加仅服务端可见的安全原因码日志，区分未知账号、错误密码和超长密码；核对日志不含用户名原文、密码、Token 或请求体，错误密码与未知账号对外继续同体 401（AUTH-01、AUTH-06、AC31）。
- [x] 12.3 在服务端配置与 Compose 传递中增加默认未启用的 DEV_PUBLIC_ORIGIN；只接受 PUBLIC_ORIGIN=http://localhost:8080、SESSION_COOKIE_SECURE=false、DEV_PUBLIC_ORIGIN=http://localhost:5173 的明确本地组合，其他非空组合启动失败；保留 Origin/Referer/Sec-Fetch-Site 规则，不开放 CORS（AUTH-05、AC32）。
- [x] 12.4 配置 Vite host=localhost、port=5173、strictPort=true，将 /api/* 原路径代理至 http://localhost:8080；代理保留 Origin、Referer、Sec-Fetch-Site 和 Cookie，不把外站来源改写为可信来源；前端继续使用相对 /api 地址并对登录 429 显示稍后重试提示（RUN-05、WEB-01、WE06）。
- [x] 12.5 扩展经 Nginx 和 Vite 的独立验收：执行 AC31、AC32、WE06，核对限额内统一 401、超限统一 429 与日志安全，5173 登录/列表/上传/登出、外站 Origin 403、8080 入口照常工作；更新 README 的本地开发步骤、DEV_PUBLIC_ORIGIN 和限流行为，保存逐场景结果后再勾选本节任务。
