## Context

仓库目前只有 OpenSpec 配置和技能，无应用代码、测试、旧数据模型或既有能力；这是基础设施与业务纵向切片的新建。动机见 proposal.md。所有技术栈与信任边界以本次用户约束为准。

## Goals / Non-Goals

**Goals:** 每次请求从服务端 Session 恢复身份；Go 统一实施 RBAC 和班级隔离；文件仅经授权 API 读取；成功上传同时产生原文件及两张表记录；启动与验收可重复。

**Non-Goals:** 完整排除项见 proposal.md 的 Non-goals。本设计将一份材料的提取文本保存为一条 knowledge_entries（.md/.txt 为 UTF-8 原文，PDF/DOCX 为解析结果），不提供知识库查询接口或后续 AI 架构。无 Flask、SQLite、Jinja/服务端模板渲染，无多职责单容器。

## Decisions

### D1 固定服务拓扑与目录

```text
Browser --http://localhost:8080--> web (Nginx :80)
                                   |-- / -> React 18 静态 dist / SPA fallback
                                   |-- /api/*、/health -> api (Go net/http :8081)
                                   |                      |-- db (MySQL 8 :3306)
                                   |                      `-- /uploads 私有持久化卷
                                   `-- /uploads、/uploads/* -> 404
```

拟新增 frontend/（React 18、TypeScript、Vite）、backend/（cmd/api、internal、migrations、测试）、deploy/nginx.conf、compose.yaml、tests/acceptance/、README.md、.env.example。Go 使用 net/http + database/sql、MySQL 驱动和 golang.org/x/crypto/bcrypt；无需额外 Web 框架。前端构建阶段运行 npm ci 与 Vite，产物复制入 Nginx 镜像；Go 独立多阶段镜像；db 使用 MySQL 8.0 且版本至少 8.0.16，以执行 CHECK。具体镜像补丁版本与 Go 工具链在实现时锁定，不改变既定技术栈。

Compose 仅 web 声明 ports: 8080:80；api/db 无 ports。api 通过服务名 db 访问数据库；Nginx 通过 api:8081 原样转发路径。web 不挂载 uploads 卷。db_data 和 uploads 两个命名卷持久化。数据库健康后 API 执行迁移和 seed，完成后提供服务；web 等待 API 健康。/health 以 2 秒超时 Ping 数据库，准备就绪返回 200 {"status":"ok"}，不可用返回 503 通用错误，绝不输出连接秘密。

本机前端开发单独运行 Vite，固定 host=localhost、port=5173、strictPort=true；server.proxy 将 /api/* 原路径转到 http://localhost:8080，由既有 Nginx 再代理 Go。Vite 代理保留浏览器的 Origin、Referer、Sec-Fetch-Site，不把外站来源改写为允许来源；浏览器仍只访问自身 5173 来源的 /api/*，Cookie 保持同源。Compose 可在明确启用本地开发时把 DEV_PUBLIC_ORIGIN 传给 API，正常部署仍只有 web 发布 8080。

### D2 信任边界与身份

浏览器输入、query/path/body、Cookie 中的任何身份声明都不可信。唯一 Cookie 为 campusclaw_session，值为 crypto/rand 生成的 32 随机字节经 base64url 编码；不是自增值，不携带 user_id/role/class_id。sessions.session_id MUST 仅保存对 Cookie token 字符串原始 ASCII 字节计算的 SHA-256 摘要，以 64 个小写十六进制字符编码并唯一索引；数据库 MUST NOT 保存原始 token；请求时对 token 求摘要查 sessions，JOIN users，检查 expires_at 后形成服务端当前用户。摘要仍对应不可预测的 Session，数据库 id 不对外使用。

每次请求从 users 读取最新 role/class_id，禁用客户端 role/class_id、JWT 或 signed-cookie 身份授权。Session 固定 24 小时过期，登录生成新 token 并撤销浏览器旧 token 对应 Session，登出删除当前 Session。Cookie 固定 HttpOnly、SameSite=Lax、Path=/、Max-Age=86400，不设置 Domain；本地 HTTP Secure=false，生产 HTTPS 强制 SESSION_COOKIE_SECURE=true。清理 Cookie 使用相同属性、Max-Age=-1 和过去 Expires。响应不返回 token，认证响应及受保护数据设置 Cache-Control: no-store。

POST 路由实施同源检查：提供 Origin 时必须精确匹配当前允许来源（正常运行仅 PUBLIC_ORIGIN）；缺少 Origin 时若有 Referer 必须与允许来源同源，Sec-Fetch-Site=cross-site 必须拒绝；Origin 优先，不要求同时提供 Referer，也不在 Origin 失败后回退 Referer。三头均缺失允许正常 Cookie 客户端测试；仅有 Sec-Fetch-Site 时接受 same-origin/none，拒绝其他值。空/非法 Origin 或 Referer 拒绝，Referer 按 scheme/host/有效 port 比较同源。生产 Origin 明确配置 HTTPS，不信任任意 Host/X-Forwarded-*。该检查在受保护路由认证及角色校验之后、业务变更之前执行，失败返回 403，保证无登录 401、学生上传 403 的契约。登录也执行同源检查。Nginx 不开放跨域 CORS。

DEV_PUBLIC_ORIGIN 默认未启用；仅当 PUBLIC_ORIGIN=http://localhost:8080、SESSION_COOKIE_SECURE=false 且 DEV_PUBLIC_ORIGIN 精确为 http://localhost:5173 时，允许在现有来源之外接受本地 Vite 来源。其他非空组合启动失败，生产 HTTPS 不接受开发来源。来源判定不信任 Host/X-Forwarded-*，不新增 CORS。

Nginx 在 http 上下文使用 $binary_remote_addr 建立共享限流区，在精确 /api/login 位置应用 10r/m、burst=5、nodelay，并保持原路径代理；按直接连接的客户端 IP 统计所有登录请求，不以用户名或 X-Forwarded-* 为键。超限在代理前返回 429 与固定 JSON rate_limited 错误、Cache-Control: no-store，不设置 Cookie。Nginx 记录拒绝事件；Go 为未知账号、错误密码、超长密码记录不含用户名原文和秘密的原因码，客户端仍只见统一 401。限流区随 Nginx 重启清空，不增加业务表。

bcrypt cost=12；未知用户名用固定的非账号 dummy bcrypt hash 做比较以缩小时间差；用户名不存在和密码错均 401 同一错误体。password 按原始字节处理，不截断或 trim；超过 bcrypt 72 字节上限不送入哈希函数，登录统一 401。seed 必填环境变量密码为 1..72 UTF-8 字节，不合规则启动失败；日志不得记录密码、token、DSN 或文件全文。

选择服务端 Session 是为了可撤销和可信租户恢复；拒绝可信 Cookie/localStorage 角色、前端单独授权和无状态身份方案。

### D3 最小数据模型

全部表使用 InnoDB、utf8mb4、UTC DATETIME(6) 时间，id 为 BIGINT UNSIGNED 主键自增。所有以下字段均 NOT NULL；时间默认当前 UTC，业务外键默认 RESTRICT，不自动级联删除。为避免 JavaScript 精度问题，API 中所有 BIGINT ID 序列化为十进制字符串。

| 表 | 字段及约束 |
| --- | --- |
| classes | id, name VARCHAR(100) UNIQUE, created_at |
| users | id, username VARCHAR(100) UNIQUE（区分大小写的 collation）, password_hash VARCHAR(255), role VARCHAR(16) CHECK IN ('teacher','student'), class_id FK classes(id), created_at；INDEX(class_id)，UNIQUE(id,class_id) |
| sessions | id, session_id CHAR(64) ASCII 二进制比较 UNIQUE, user_id FK users(id), created_at, expires_at；INDEX(user_id)，INDEX(expires_at) |
| materials | id, class_id FK classes(id), uploaded_by, title VARCHAR(255), original_filename VARCHAR(255), stored_filename VARCHAR(255) UNIQUE, content_type VARCHAR(100), created_at；INDEX(class_id)，UNIQUE(id,class_id)，复合 FK(uploaded_by,class_id) REFERENCES users(id,class_id) |
| knowledge_entries | id, class_id FK classes(id), material_id, content MEDIUMTEXT, created_at；INDEX(class_id)，UNIQUE(material_id)，复合 FK(material_id,class_id) REFERENCES materials(id,class_id) |

数据库 strict SQL mode 开启，不以缺省 class_id 掩盖缺失；CHECK 限制角色，NOT NULL 禁止无班级业务行，复合外键防止材料/知识条目及上传者班级不一致。一材料仅一条全文，MEDIUMTEXT 容纳最大 5 MiB 提取文本；对 PDF/DOCX 的解析输出独立执行 5 MiB UTF-8 字节上限。不建未来业务表；允许迁移工具自身的版本元数据表。

按版本执行有序迁移；MySQL DDL 不假装具备完整事务回滚，重复启动只运行未应用版本，不 DROP/TRUNCATE。seed 使用事务、classes.name 与 users.username 的唯一约束保证幂等，以既有行作为权威，冲突不覆盖密码/角色/班级，不清理材料。

| 名称 | username | 班级 | role | 服务端环境变量 |
| --- | --- | --- | --- | --- |
| Teacher A | teacher_a | Class A | teacher | SEED_TEACHER_A_PASSWORD |
| Student A1 | student_a1 | Class A | student | SEED_STUDENT_A1_PASSWORD |
| Teacher B | teacher_b | Class B | teacher | SEED_TEACHER_B_PASSWORD |
| Student B1 | student_b1 | Class B | student | SEED_STUDENT_B1_PASSWORD |

四个种子账号仅在新建时生成 bcrypt hash；重复 seed 不重置密码，包括新加入的 teacher_b。若预存同名 seed 账号的角色/班级不符则失败报错而非静默改写。缺少 SEED_TEACHER_B_PASSWORD 与缺少其他必填种子密码一样导致启动失败，不使用默认密码。密码与 MYSQL_PASSWORD、MYSQL_ROOT_PASSWORD 等只从环境读取；.env 被 gitignore/dockerignore 排除，.env.example 只列空值与说明，无可用密码，前端不得注入任何秘密。

### D4 固定 API 契约

JSON 错误统一 {"error":{"code":"...","message":"..."}}，不附资源存在性/SQL/路径。404 固定 not_found，跨班与不存在使用同样查询失败分支、状态码和响应体，不先做全局存在性检查。401 固定 unauthorized；403 固定 forbidden；登录限流 429 固定 rate_limited 与通用提示。未知 API 不回 SPA。方法错误 405。详情/下载共用 ID 校验：只接受 ASCII 十进制数字串，解析为 1..18446744073709551615 的 BIGINT UNSIGNED；前导零允许（001 等价 1），非数字、空格、正负号、0、负数、小数、科学计数和溢出均返回相同 404 not_found。先完成认证，故未登录无效 ID 仍 401。

User={id,username,role,class_id,class_name}。Material={id,class_id,uploaded_by,title,original_filename,content_type,created_at}，不返回 stored_filename/磁盘路径。列表按 created_at DESC,id DESC；本迭代不提供搜索或分页参数。

| 方法与路径 | 输入 | 成功响应 | 失败 |
| --- | --- | --- | --- |
| POST /api/login | JSON username,password（其他身份字段不用于授权） | 200 {user:User} + Cookie | 凭证错误/未知用户 401；JSON 非法/必填字段缺失 400；Nginx 限流 429 |
| POST /api/logout | 当前 Cookie | 204 空体并删除 Session/清 Cookie | 未登录/过期 401 并清 Cookie |
| GET /api/me | 当前 Cookie | 200 User | 未登录/过期 401 |
| GET /api/materials | 当前 Cookie | 200 {materials:Material[]} | 未登录 401 |
| GET /api/materials/{id} | 正整数 ID | 200 {material:Material,content:string} | 未登录 401；无效 ID/跨班/不存在 404 |
| GET /api/materials/{id}/file | 正整数 ID | 200 原始文件，attachment | 未登录 401；无效 ID/跨班/不存在/原文件缺失 404 |
| POST /api/materials | multipart title + 唯一 file | 201 {material:Material}，Location=/api/materials/{id} | 未登录 401；student 403；字段/路径/格式不匹配或损坏 400；容量/页数/解压限额超限 413；扩展名不支持 415；加密/无可提取正文/解析超时 422；存储/事务或解析器部署故障 500；解析槽位等待超时 503 |
| GET /health | 无 | 200 {status:"ok"} | 未就绪 503 |

所有 /api/materials 路由忽略 query 中 class_id、uploaded_by、role（它们不是可用筛选参数）；上传表单中这些伪造字段也固定忽略，永远用当前用户赋值。无新增班级 path 参数。受保护请求顺序：认证 → 上传则检查 teacher → 输入校验 → 携带服务端 class_id 的仓储查询。学生上传即使 multipart 非法也先返回 403。

列表 WHERE class_id=?；详情/下载 WHERE id=? AND class_id=?；详情读取知识条目也同时限定 material_id=? AND class_id=?。仓储接口必须显式接受服务端 tenant ID，不提供业务 handler 可调用的无租户材料查询。跨班统一 404，仅上传角色禁止用 403。

### D5 上传、文档解析与事务

支持 .md/.txt/.pdf/.docx（后缀大小写归一）；.doc/.docm/.exe/.zip 等其他后缀 415。仍为单文件 <=5 MiB、整个 multipart <=6 MiB、title trim 后 1..255 字符；Go 与 Nginx 限制一致。所有格式共享认证、RBAC、班级隔离和原文件下载 API，不新增解析端点、上传状态表或服务。

原始 filename <=255 字符，拒绝 /、反斜杠、.. 路径段、绝对盘符、NUL/控制字符。安全保存到 /uploads/{current_class_id}/{crypto-random-UUID}_{safe_filename}；safe_filename 仅允许字母数字/下划线/短横线/点，其余替换，按 UTF-8 字节边界截断至 180 字节并保留后缀，stored_filename 只保存 basename，总长 <=255 字节。目录 0700、文件 0600、排他创建，不跟随外部可控符号链接，所有打开/删除路径均验证在本班目录内；Nginx 不挂载原文件卷。

| 格式 | 校验与提取 | 服务端 content_type |
| --- | --- | --- |
| .md | 完整有效 UTF-8、至少一个字节、无 NUL；原样存储文本 | text/markdown |
| .txt | 与 .md 相同 | text/plain |
| .pdf | PDF 结构可解析，提取文本层，排除加密与无可提取文本文件 | application/pdf |
| .docx | 合法无宏 OOXML Word ZIP 包，解析主文档的正文段落与表格文本 | application/vnd.openxmlformats-officedocument.wordprocessingml.document |

客户端 Content-Type 只作提示，不作授权或格式证明。文本允许 PK、MZ、%PDF- 等普通字面量开头，不能仅因短前缀误判。可补充典型二进制检测（SHOULD），但不得否定合法 UTF-8、非空、无 NUL 的普通文本；明显伪装的 PNG/二进制 fixture 由 UTF-8/NUL 验证拒绝。PDF/DOCX 的二进制原文件不应用文本 UTF-8/NUL 规则，必须走对应结构解析。

PDF 固定使用 API 容器内 Poppler 的 pdfinfo 与 pdftotext，由 Go os/exec 直接传参数调用，不经 shell，不传用户原始文件名作参数。pdfinfo 检查页数 <=200 与加密标识；pdftotext -enc UTF-8 -eol unix -nopgbrk <服务器生成的绝对路径> - 将文本写 stdout。合计解析时间 <=10 秒（包括元数据检查），stdout 有界读取 <=5 MiB；超限/超时立即终止进程并等待回收，stderr 最多保留 64 KiB 供内部分类，不回传给用户或记录正文。禁止传解密密码，所有加密 PDF 返回 422；扫描图像型或提取后全空白 PDF 返回 422，不执行 OCR。混合 PDF 仅提取文本层，图像内容不纳入知识库；不承诺多栏/图表版式还原。

DOCX 固定使用 Go archive/zip + encoding/xml，不调用 Office/LibreOffice、不解压到磁盘、不访问包外 URL。验证 [Content_Types].xml 和 _rels/.rels，按 officeDocument 内部关系解析主文档路径（不能假定必为 word/document.xml）；主 part 必须是无宏 WordprocessingML document content type。支持常见 Transitional 与 Strict WordprocessingML 命名空间。拒绝缺失/重复 part、损坏 ZIP/CRC/XML、非法关系路径、DOCTYPE/自定义实体、宏内容、外部主文档关系；普通外链超链接只保留显示文字，绝不抓取目标。对于 .docx，若检测到 OLE/CFB 容器签名（包括加密 OOXML 包和伪装的旧版 Word），统一返回 422 unsupported_office_container，不尝试解密或支持旧版格式；其他非 ZIP 伪装返回 400。

DOCX 限制 <=1000 ZIP entries、所有 entries 声明解压总大小 <=20 MiB，实际读取的累计解压字节也 <=20 MiB（不能仅信元数据）；XML 深度 <=128、解析总时长 <=10 秒、提取 UTF-8 文本 <=5 MiB，超容量/深度返回 413、超时返回 422。解码过程检查 deadline 与有界 reader；不创建无法终止的后台解析 goroutine。遍历正文按文档顺序连接 w:t，处理 w:tab/w:br；段落换行、表格按行输出且单元格用制表符分隔。不提取页眉页脚、批注、删除修订、文本框、图片和嵌入对象，不执行宏/外链/脚本；正文中保留的插入文字正常读取。不保留样式、分页或合并单元格布局，正文提取后全空白返回 422。

输出对 PDF/DOCX 必须为有效 UTF-8、无 NUL、含非空白文本，统一换行为 LF；.md/.txt 原文不变。错误归类：不支持后缀 415；损坏或格式不匹配 400；文件/请求/页数/解压/输出/深度超限 413；加密、无可提取文本、解析超时 422；I/O、解析器缺失或内部故障 500。所有解析失败都不创建两张业务表记录，并尝试清理已保存原文件；用户只看到固定业务错误，不暴露解析器输出和路径。

Go 负责认证、存储、解析调用和数据库事务，Poppler 仅为 API 容器内离线文档工具，不替代 Go 后端。API 运行非 root；镜像包含固定版本 poppler-utils，启动时检查两个命令可用，缺失则启动失败。Compose API 设置内存 512 MiB、CPU 1、pids_limit 64；PDF/DOCX 解析每进程最多同时一个，等待槽位最长 2 秒，超时返回 503 busy 并清理本次文件。Nginx proxy_read_timeout=30s、Go WriteTimeout=30s，留出解析和事务时间；容量与超时测试用可控 fixture/注入，无需依赖机器性能碰运气。

流程：Session → teacher → class_id → 有界接收/基础校验 → 安全保存原文件 → 对应解析器提取文本 → BEGIN → INSERT materials → INSERT knowledge_entries（同班级，一份提取文本）→ COMMIT → 201。仅 COMMIT 成功返回 201，同班下一次查询立即可见，下载始终返回原始字节而非转换文本。

事务边界简化：明确的 INSERT/事务失败 MUST ROLLBACK 并尝试删除本次原文件；删除失败记录不含正文/秘密的错误，响应仍失败。COMMIT 结果未知时返回 500、保留文件并记录需人工核对，MUST NOT 盲删可能已提交的文件。本迭代不做独立连接自动核对、24 小时定时/启动扫描或上传状态机；这些为后续 SHOULD 强化项，不生成必做任务。运维说明给出先核对数据库引用再人工处理孤立文件的原则。MA03 仅验证写失败、删除失败与未知提交的最小安全行为。

下载先认证并以 id+class_id 查询，再由 Go 安全打开文件；四种格式一律 Content-Disposition: attachment、nosniff、no-store。详情仅返回知识库提取文本，React 纯文本显示，无 PDF/Word 嵌入式预览。

解析依据：[Poppler pdftotext 手册](https://manpages.debian.org/trixie/poppler-utils/pdftotext.1.en.html)、[pdfinfo 手册](https://manpages.debian.org/trixie/poppler-utils/pdfinfo.1.en.html)、[WordprocessingML 结构](https://learn.microsoft.com/en-us/office/open-xml/word/structure-of-a-wordprocessingml-document)、[Go ZIP](https://pkg.go.dev/archive/zip) 与 [XML](https://pkg.go.dev/encoding/xml)。上述大小/时限及支持范围是本项目约定，不是工具默认保证。

### D6 React 状态与交互

应用启动唯一身份来源为 GET /api/me。200 恢复 User 并加载材料；401 清内存进入 Login Page；网络/5xx 展示可重试错误，不伪装为未登录。使用相对 /api URL、同源 Cookie；不在 localStorage 存 token 或可信 role/class_id。Login 成功进入 Materials Page，显示 username、role、class_name/class_id、列表、查看/下载与登出。登录收到 429 时显示通用稍后重试提示，不伪装为错误密码或自动反复重试。详情在材料页文本面板呈现。

teacher 显示 title、accept=.md,.txt,.pdf,.docx 文件输入及提交状态，201 后重新请求列表；student 无上传 UI。学生即使手改 UI、localStorage 或直接 API 仍由 Go 拒绝。任何受保护请求 401 则退出当前内存身份；403/404 显示通用错误。登出 204 或已失效 401 均清状态。自动化使用浏览器测试 UI 与同源网络，通过 Cookie jar 直接 HTTP 请求测试服务端安全边界。

## Risks / Trade-offs

- [文件与数据库无法原子提交] → 明确失败时回滚/尝试删除，提交不明保留文件并记录人工核对；自动恢复推迟，可能暂留孤立文件，绝不盲删已提交文件。
- [Session 表增长] → 定期删除过期 Session，查询始终校验 expires_at，清理延迟不会延长有效期。
- [本地 HTTP 无 Secure] → 本地仅开发配置；生产 HTTPS 必须开启 Secure 与固定 PUBLIC_ORIGIN。
- [文本可能包含 HTML/脚本] → 详情仅显示文本，下载 attachment + nosniff，禁止 Nginx 原文件公开。
- [数据库约束依赖 MySQL 版本/模式] → MySQL 8.0.16+、strict mode，在真实 MySQL 上跑 NULL、CHECK、复合 FK 集成测试，不使用 SQLite 替代。
- [登录耗时和材料列表规模] → 当前教学试点使用同步 bcrypt 与完整列表；不把搜索、分页或后续 AI 加入本迭代。

## Migration Plan

1. 完成配置说明，用户复制 .env.example 为未跟踪 .env 并设置数据库与四账号密码；这是秘密配置前提，不需要手动编译、执行 SQL 或创建容器。
2. docker compose up --build 启动 db、api、web；迁移与 seed 自动执行，http://localhost:8080 可用，GET /health=200。
3. 真实 MySQL 集成测试使用独立测试库/卷和运行时临时秘密；通过 Nginx 运行 spec 场景，不改用户业务数据。故障注入仅测试构建/测试连接，不增加生产故障触发 API。
4. 重启与重复初始化验证账号/班级计数、密码 hash、材料记录及文件保持；正常停止使用 docker compose down，不带 -v。
5. 发布前备份 db_data 与 uploads。此首次建库无旧数据转换；回退应用镜像保留两个卷，禁止通过删库恢复。若需数据恢复，同时恢复同一时点的数据库和文件备份。


文档解析限制需在材料页说明：最大 5 MiB，PDF/DOCX 只提取支持范围内的文本，不执行 OCR；失败不得显示上传成功。


