# CampusClaw

教师与学生共用的教学材料平台：教师登录后上传 Markdown、纯文本、PDF 与 DOCX 材料，
系统保存原文件并提取文本；同班学生可以查看、下载本班材料。

身份与班级边界全部由 Go 后端在每次请求时从数据库重新确定，浏览器只通过 Nginx
的 8080 端口访问，无法直达 API、数据库或上传文件。

---

## 1. 技术栈与拓扑

```text
Browser --http://localhost:8080--> web (Nginx :80)
                                   |-- /                -> React 18 静态产物 / SPA fallback
                                   |-- /api/*、/health  -> api (Go net/http :8081)
                                   |                        |-- db (MySQL 8 :3306)
                                   |                        |-- qdrant (向量库 :6333)
                                   |                        |-- 嵌入网关 / 对话网关（服务端调用）
                                   |                        `-- /uploads 私有持久化卷
                                   `-- /uploads、/uploads/* -> 404
```

- 前端：React 18 + TypeScript + Vite（镜像内 `npm ci` 后构建，产物复制进 Nginx）
- 后端：Go `net/http` + `database/sql` + MySQL 驱动 + `golang.org/x/crypto/bcrypt`
- 数据库：MySQL 8.0（≥ 8.0.16，使用 CHECK 约束）
- 向量库：Compose 内的私有 Qdrant，只存向量与标识；切片正文始终在 MySQL
- 模型网关：兼容 OpenAI `/embeddings` 与 `/chat/completions` 的服务端网关，
  由浏览器之外调用，密钥只存在于服务端配置
- 文档解析：PDF 走 API 镜像内的 Poppler（`pdfinfo` / `pdftotext`）；DOCX 走 Go 标准库
  `archive/zip` + `encoding/xml`

Compose 只发布 `web` 的 `8080:80`；`api`、`db` 与 `qdrant` 没有任何宿主机端口，
浏览器无法直达向量库或两类模型网关。

---

## 2. 快速开始

### 2.1 配置

```bash
cp .env.example .env
```

然后填写 `.env` 中的每一个变量。`.env` 已被 `.gitignore` 与 `.dockerignore` 排除，
仓库与镜像中不会出现真实密码。

| 变量 | 说明 |
| --- | --- |
| `MYSQL_HOST` / `MYSQL_PORT` | 容器内填写 `db` / `3306` |
| `MYSQL_DATABASE` | 数据库名 |
| `MYSQL_USER` / `MYSQL_PASSWORD` | API 使用的数据库账号 |
| `MYSQL_ROOT_PASSWORD` | 仅 db 容器初始化使用，API 不读取 |
| `SEED_TEACHER_A_PASSWORD` | 必填，1..72 UTF-8 字节 |
| `SEED_STUDENT_A1_PASSWORD` | 必填，1..72 UTF-8 字节 |
| `SEED_TEACHER_B_PASSWORD` | 必填，1..72 UTF-8 字节 |
| `SEED_STUDENT_B1_PASSWORD` | 必填，1..72 UTF-8 字节 |
| `PUBLIC_ORIGIN` | 浏览器入口来源，例如 `http://localhost:8080`；POST 同源校验的比对基准 |
| `DEV_PUBLIC_ORIGIN` | 可选；仅本地 HTTP 开发时填写 `http://localhost:5173`，生产保持空值 |
| `QDRANT_URL` | 可选；默认 `http://qdrant:6333`，容器内按服务名访问 |
| `QDRANT_COLLECTION` | 可选；默认 `campusclaw_chunks`。切换嵌入模型时可指定新集合，填充向量后再切换 API，旧集合保留供回退 |
| `EMBEDDING_BASE_URL` | 必填；兼容 OpenAI `POST {BASE_URL}/embeddings` 的网关地址 |
| `EMBEDDING_MODEL` / `EMBEDDING_API_KEY` | 必填；嵌入模型名与密钥，仅 API 读取 |
| `EMBEDDING_DIMENSIONS` | 必填；必须等于嵌入模型实际返回的宽度，不一致时 API 启动失败而不是混写不同维度向量 |
| `CHAT_BASE_URL` | 必填；兼容 OpenAI `POST {BASE_URL}/chat/completions` 的网关地址 |
| `CHAT_MODEL` / `CHAT_API_KEY` | 必填；对话模型名与密钥，仅 API 读取 |

缺少任一必填变量时，`docker compose up` 会直接报出缺少的变量名并停止启动，
错误信息中不含任何值。两类网关密钥不会出现在前端构建产物、浏览器响应或日志中。

### 2.2 启动

```bash
docker compose up --build
```

不需要手工执行 SQL、迁移或前端构建。启动后：

- <http://localhost:8080> 打开登录页
- <http://localhost:8080/health> 返回 `{"status":"ok"}`

停止：`docker compose down`（**不要加 `-v`**，那会删除数据卷）。

### 2.3 种子账号

| 用户名 | 角色 | 班级 | 密码来源 |
| --- | --- | --- | --- |
| `teacher_a` | teacher | Class A | `SEED_TEACHER_A_PASSWORD` |
| `student_a1` | student | Class A | `SEED_STUDENT_A1_PASSWORD` |
| `teacher_b` | teacher | Class B | `SEED_TEACHER_B_PASSWORD` |
| `student_b1` | student | Class B | `SEED_STUDENT_B1_PASSWORD` |

初始化是幂等的：重复启动不会重复创建账号、不会重置已存在的密码、不会清理材料。
若已存在的同名账号角色或班级与预期不符，seed 会报错停止，而不会静默改写。
### 2.4 本地 Vite 开发

先按 2.1 配置四个种子密码，并设置 `PUBLIC_ORIGIN=http://localhost:8080`、
Set PUBLIC_ORIGIN=http://localhost:8080 and DEV_PUBLIC_ORIGIN=http://localhost:5173 before starting Compose.
另开终端运行：

```bash
cd frontend
npm ci
npm run dev
```

浏览器打开 <http://localhost:5173>。Vite 固定使用 5173；`/api/*` 原路径代理到
Nginx 的 8080 入口，浏览器始终向自己的 5173 来源发送请求。端口被占用时 Vite
会报错，不会自动换端口。代理保留 `Origin` 等来源头，后端只在上述本地配置下
额外接受 5173；生产环境必须让 `DEV_PUBLIC_ORIGIN` 保持空值。

登录入口按直接客户端 IP 限制为持续 10 次/分钟、额外突发 5 次；超限返回统一
JSON 429，并提示稍后重试。账号不存在和密码错误在未超限时仍返回相同的 401。

---

## 3. API

| 方法 | 路径 | 成功 | 主要失败 |
| --- | --- | --- | --- |
| POST | `/api/login` | 200 `{user,token,expires_at}`，不设置任何 Cookie | 401 invalid credentials; 400 invalid JSON; 429 rate limit |
| POST | `/api/logout` | 204, revokes the session | 401 without a valid Bearer token |
| GET | `/api/me` | 200 `User` | 401 |
| GET | `/api/materials` | 200 `{materials:[]}` | 401 |
| GET | `/api/materials/{id}` | 200 `{material,content}` | 401；无效 ID/跨班/不存在 404 |
| GET | `/api/materials/{id}/file` | 200 原文件 | 401；无效 ID/跨班/不存在/文件缺失 404 |
| POST | `/api/materials` | 201 `{material}` + `Location` | 见下 |
| POST | `/api/search` | 200 `{query_vector_generated,message,hits:[]}` | 401；空 query/未知 mode 400；向量依赖不可用 503 |
| POST | `/api/ask` | 200 `{answer,citations:[]}` | 401；空问题 400；检索或对话网关失败 503 |
| POST | `/api/materials/{id}/reindex` | 200 `{index}` | 401；学生 403；非法切分参数 400；跨班/不存在 404 |
| GET | `/api/materials/{id}/index` | 200 `{index}` | 401；学生 403；跨班/不存在 404 |
| GET | `/health` | 200 `{"status":"ok"}` | 503 |

检索与问答：

- `mode` 取 `keyword`、`vector`、`hybrid`，缺省为 `hybrid`。`keyword` 只走 MySQL 全文索引，
  不调用嵌入网关；`vector` 与 `hybrid` 需要向量库与嵌入网关。
- 检索范围只取服务端会话中的 `class_id`：请求体、query 参数或 header 里的 `class_id`
  一律忽略，跨班内容表现为 200、`hits=[]`，不以 403/404 暗示其存在。
- 每条命中给出材料 ID 与标题、切片 ID 与序号、字符区间、偏移基准（`extracted`/`normalized`）、
  取自 MySQL 的纯文本摘录，以及指向授权材料详情的 `source`。响应**不含**任何向量分量。
- 无合格候选时返回 200、`hits=[]` 与固定文案「资料中未找到相关内容」，不用低相关度候选凑数。
- `/api/ask` 只用最新一条问题做本班 hybrid 检索并取前 4 条；没有命中时直接返回固定文案与空
  `citations`，**完全不调用对话模型**。客户端提交的 `system` 消息一律丢弃。
- 向量库或网关不可用时，`keyword` 仍可用，`vector`/`hybrid`/`ask` 返回通用 503，
  不降级为别的模式，也不泄露连接地址、密钥或跨班内容。

登录鉴权只用 `Authorization: Bearer <token>`：浏览器把服务端签发的不透明 token 保存在 `localStorage`，**链路中没有任何 Cookie，也没有刷新接口**。页面刷新后由 `GET /api/me` 重新校验该 token；会话在登录后固定 24 小时过期，到期即 401，需要重新登录。迁移会清空旧版 Cookie 时期的会话行，因此切换后所有用户需重新登录一次。

错误统一为 `{"error":{"code":"...","message":"..."}}`。`404` 固定 `not_found`，
跨班与不存在使用完全相同的响应体；`401` 固定 `unauthorized`；`403` 固定 `forbidden`；登录限流 `429` 固定 `rate_limited`。
未知 `/api/*` 路径返回 JSON 404，不会回退成 SPA。

`User` 为 `{id,username,role,class_id,class_name}`；
`Material` 为 `{id,class_id,uploaded_by,title,original_filename,content_type,created_at}`，
**不含** `stored_filename` 或任何磁盘路径。所有 BIGINT ID 序列化为十进制字符串。

### 上传失败状态码

| 状态码 | 含义 |
| --- | --- |
| 400 | 字段缺失、文件名危险（含路径穿越/绝对路径）、文本非法（非 UTF-8、含 NUL、空文件）、文档结构损坏或格式伪装 |
| 401 | 未登录 |
| 403 | 学生上传；跨站来源 |
| 413 | 文件超 5 MiB、请求超 6 MiB、PDF 超 200 页、DOCX 超 1000 个 entry / 解压超 20 MiB / XML 深度超 128 / 提取文本超 5 MiB |
| 415 | 扩展名不在 `.md`/`.txt`/`.pdf`/`.docx` 之内 |
| 422 | 加密 PDF、以 `.docx` 上传的 OLE/CFB 容器、扫描件或无可提取正文、解析超 10 秒 |
| 500 | 存储/事务失败、解析器缺失或内部故障 |
| 503 | 解析槽位等待超过 2 秒 |

学生上传即使 multipart 非法也在读取请求体之前返回 403。

---

## 4. 文档解析支持范围与限额

| 格式 | 校验与提取 | 存储的 `content_type` |
| --- | --- | --- |
| `.md` | 完整有效 UTF-8、至少一个字节、无 NUL；原样保存文本 | `text/markdown` |
| `.txt` | 同上 | `text/plain` |
| `.pdf` | 结构可解析，提取文本层；`pdfinfo` 检查页数与加密标识 | `application/pdf` |
| `.docx` | 合法无宏 OOXML 包，按正文顺序提取段落与表格文字 | `application/vnd.openxmlformats-officedocument.wordprocessingml.document` |

- 单个文件 ≤ 5 MiB，整个 multipart 请求 ≤ 6 MiB，`title` trim 后 1..255 字符。
- 后缀大小写不敏感；`.doc`、`.docm`、`.zip`、`.exe` 等一律 415。
- **不执行 OCR**：扫描图像型 PDF 与仅含图片的 DOCX 会上传失败（422）。
- PDF 只提取文本层，图像内容不进入知识库；不承诺多栏/图表版式还原。
- DOCX 不提取页眉页脚、批注、修订历史、文本框、图片与嵌入对象；不执行宏、脚本或外链抓取。
- 客户端 `Content-Type` 仅作提示，服务端按扩展名重新判定并保存自己的类型。
- 文本内容不会被短魔数误判：以 `PK`、`MZ`、`%PDF-` 开头的普通文本仍然合法。

一份材料对应一条提取文本（`knowledge_entries`），原文件与两条记录在一次上传中一并落库：
原文件保存成功且解析成功后，`materials` 与 `knowledge_entries` 在同一事务中提交，
两者要么都存在、要么都不存在。

---

## 5. 测试

本机未安装 Go，因此所有 Go 命令都在 Docker 中执行。下面假定当前目录为仓库根。

> **Git Bash / MSYS 用户请注意**：MSYS 会把容器内的路径参数（如 `-w /src`）改写成
> Windows 路径，导致 `docker: the working directory '...' is invalid`。下面每条命令都
> 以 `MSYS_NO_PATHCONV=1` 开头来关闭该改写。
>
> **PowerShell / cmd / Linux / macOS 不需要这个前缀**——PowerShell 会把它当作语法错误，
> 照抄时请删除行首的 `MSYS_NO_PATHCONV=1 `（PowerShell 本身不会改写路径参数）。

### 5.1 构建测试镜像

```bash
MSYS_NO_PATHCONV=1 docker build -t campusclaw-zwh-test:latest backend/tests
```

该镜像在 `golang:1.25-alpine` 之上安装了 `poppler-utils` 与 `poppler-data`，
与 API 运行镜像的解析环境一致。

### 5.2 启动隔离的测试数据库

测试库位于独立网络、独立容器，**没有宿主机端口**，也不会触碰业务数据卷：

```bash
MSYS_NO_PATHCONV=1 docker network create campusclaw_zwh_test
MSYS_NO_PATHCONV=1 docker run -d --name campusclaw_zwh_test_db --network campusclaw_zwh_test \
  -e MYSQL_ROOT_PASSWORD=test-root -e MYSQL_DATABASE=campusclaw_test \
  -e MYSQL_USER=campusclaw -e MYSQL_PASSWORD=test-app mysql:8.0
```

测试套件会拒绝在数据库名不以 `_test` 结尾时运行，以免误清业务库。

### 5.3 单元测试与 MySQL 集成测试

```bash
MSYS_NO_PATHCONV=1 docker run --rm --network campusclaw_zwh_test \
  -e MYSQL_HOST=campusclaw_zwh_test_db -e MYSQL_PORT=3306 \
  -e MYSQL_DATABASE=campusclaw_test -e MYSQL_USER=campusclaw -e MYSQL_PASSWORD=test-app \
  -e GOPROXY="https://goproxy.cn|https://proxy.golang.org" \
  -v "$PWD/backend:/src" -v campusclaw-gomodcache:/go/pkg/mod -w /src \
  campusclaw-zwh-test:latest \
  sh -c "go vet ./... && go test -count=1 ./..."
```

未设置 `MYSQL_HOST` 时，数据库相关的用例会自动跳过。

`GOPROXY` 必须显式设置：容器默认走 `proxy.golang.org`，在部分网络下会**长时间无响应**而不是快速失败。
条目之间用 `|` 分隔——逗号只在 404/410 时回退，管道符在任何错误（含连接超时）时都会回退。
`campusclaw-gomodcache` 卷缓存模块，重复运行不必重新下载。

### 5.4 故障注入（回滚与补偿）

失败注入的 setter 只在 `testhooks` 构建标签下编译，因此生产二进制没有任何触发入口：

```bash
MSYS_NO_PATHCONV=1 docker run --rm --network campusclaw_zwh_test \
  -e MYSQL_HOST=campusclaw_zwh_test_db -e MYSQL_PORT=3306 \
  -e MYSQL_DATABASE=campusclaw_test -e MYSQL_USER=campusclaw -e MYSQL_PASSWORD=test-app \
  -e GOPROXY="https://goproxy.cn|https://proxy.golang.org" \
  -v "$PWD/backend:/src" -v campusclaw-gomodcache:/go/pkg/mod -w /src \
  campusclaw-zwh-test:latest \
  sh -c "go test -count=1 -tags testhooks -run 'TestKnowledgeInsertFailure|TestDeleteFailure|TestUnknownCommit' ./tests/..."
```

### 5.5 HTTP 验收入口（经 Nginx）

先 `docker compose up --build`，然后：

```bash
MSYS_NO_PATHCONV=1 docker run --rm --add-host=host.docker.internal:host-gateway -v "$PWD/tests/acceptance:/src" -w /src \
  -e ACCEPTANCE_BASE_URL=http://host.docker.internal:8080 \
  -e ACCEPTANCE_TEACHER_A_PASSWORD=... \
  -e ACCEPTANCE_STUDENT_A1_PASSWORD=... \
  -e ACCEPTANCE_TEACHER_B_PASSWORD=... \
  -e ACCEPTANCE_STUDENT_B1_PASSWORD=... \
  golang:1.25-alpine sh -c "go run ."
```

该程序只用标准库，逐个覆盖 AC01–AC15、AC19、AC21、AC25–AC31，输出每个编号的
PASS/FAIL。它从不直接访问 API 容器或数据库，全部走与浏览器相同的 8080 入口与真实
Bearer access tokens cover authentication, class isolation, parsing and the gateway.

原始的数据库行数断言由 5.3 的 Go 套件在容器网络内完成（AC16–AC18、AC20、DA/MA 场景）；
本入口通过可观察后果（列表长度不变、详情 404）验证同类事实。

### 5.6 浏览器端到端（WE01–WE06、WE10–WE13、AC23/AC32）

需要一个真实浏览器，因此该套件在本机运行。它**没有任何 npm 依赖**：
`cdp.mjs` 是一个很小的 DevTools 协议客户端，直接驱动 Chromium。

```bash
cd tests/browser
node check.mjs
```

可用环境变量：

| 变量 | 说明 |
| --- | --- |
| `BROWSER_BASE_URL` | 默认 `http://localhost:8080` |
| `TEACHER_A_PASSWORD` / `STUDENT_A1_PASSWORD` / `TEACHER_B_PASSWORD` / `STUDENT_B1_PASSWORD` | 必填 |
| `CHROME_PATH` | 可选；默认使用 Playwright 缓存的 Chromium（`%LOCALAPPDATA%\ms-playwright`） |

若本机没有 Chromium，执行一次 `npx playwright install chromium` 即可（只下载浏览器，
不安装任何依赖到本项目）。若缓存的 Chromium 在本机异常退出，可将 `CHROME_PATH` 指向已安装的 Chrome。
也可按 2.4 启动 Vite，再设置 `BROWSER_BASE_URL=http://localhost:5173` 运行同一脚本；
此时浏览器验收 AC32 会核对所有业务请求均停留在 5173，外站 `Origin` 仍为 403。
脚本最后执行 WE06：触发登录 429 并核对页面只显示通用稍后重试提示。
运行 HTTP 与浏览器套件时请顺序执行；登录限流按客户端 IP 计数，常规登录已按
7 秒间隔发送，故意触发限流的场景安排在最后。

它驱动的是与用户相同的 8080 入口，并记录浏览器发出的每一个请求，用于断言所有请求
都只落在该来源上（8080 模式为 AC23，5173 模式为 AC32）。同时验证：刷新后由 `GET /api/me` 恢复登录、错误密码停留在
登录页、教师上传后列表刷新、学生看不到上传入口、篡改 `localStorage` 不改变身份、
材料中的 `<script>` 只按文本显示而不执行、登出后刷新仍未登录。

检索相关的场景：`WE13` 教师可见切分策略与索引状态、按 hierarchy 重建后仍可检索；
`WE10` 学生三种方式检索、命中可打开、空输入提示、无依据固定文案；`WE12` 回答编号与
出处一一对应且可点选；`WE11` 在 390px 视口下检索与问答可用且不产生横向滚动。

输出每个编号的 PASS/FAIL，例如：

```text
WE02 PASS  未登录进入登录页；错误密码留在登录页；正确凭证进入材料页
...
AC23 PASS  浏览器只访问 localhost:8080 这一个来源
11 checks, 0 failed
```

本次新增场景的逐项执行结果见 [验收记录](tests/evidence/add-traceable-vector-retrieval-2026-09-30.md)。

### 5.7 模型网关替身（仅验收）

`tests/gateway` 是一个确定性的、兼容 OpenAI 的嵌入与对话网关替身，**不参与产品运行**，
只为在没有真实模型服务时也能端到端验收向量、混合与问答路径。它用「概念词袋」产生向量，
因此「同义改写能被语义路径命中、而关键字路径命中不了」可以被真实地验证。

```bash
docker build -t campusclaw-zwh-gateway:latest tests/gateway
docker run -d --name campusclaw-zwh-stub-gateway -p 127.0.0.1:11434:8080 campusclaw-zwh-gateway:latest
```

把 `.env` 的 `EMBEDDING_BASE_URL` / `CHAT_BASE_URL` 指向 `http://host.docker.internal:11434/v1`，
`EMBEDDING_DIMENSIONS` 设为 1536 即可。换成真实网关地址、模型名与密钥后，代码无需改动。

验收建议使用独立的 Compose 项目（独立数据卷、干净数据库）：

```bash
DEV_PUBLIC_ORIGIN= PUBLIC_ORIGIN=http://localhost:8090 \
docker compose -p campusclaw-zwh-acc -f compose.yaml \
  -f tests/acceptance/override-8090.yaml up -d --build
```

### 5.8 前端

```bash
cd frontend
npm ci
npm run typecheck
npm run build
```

---

## 6. 数据持久化、备份与回退

三个命名卷保存全部状态：

- `campusclaw-zwh_db_data` → MySQL 数据目录（材料、原文本、切片与索引状态）
- `campusclaw-zwh_uploads` → 私有原文件，按班级 ID 分目录
- `campusclaw-zwh_qdrant_data` → 向量库数据

备份（应用停止后执行更稳妥）：

```bash
docker compose stop
docker run --rm -v campusclaw-zwh_db_data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/db_data.tgz -C /data .
docker run --rm -v campusclaw-zwh_uploads:/data -v "$PWD:/backup" alpine \
  tar czf /backup/uploads.tgz -C /data .
docker run --rm -v campusclaw-zwh_qdrant_data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/qdrant_data.tgz -C /data .
docker compose start
```

**数据库与文件必须来自同一时点**：只恢复其中一个会得到引用不存在文件（或反之）的记录。
向量卷例外：它丢失只意味着需要重建索引，MySQL 中的切片正文仍然完整——
删除向量卷后重新启动，API 会自动为所有材料重新建立索引。

回退应用版本时保留三个卷，重新构建镜像即可；不要通过删库或删卷来"恢复"。
正常停止一律使用 `docker compose down`，**不要使用 `-v`**。

---

## 7. 生产部署注意事项

- Production must use HTTPS: the Bearer token is sent on every request and is protected only by transport encryption. Local HTTP development is supported.
- `PUBLIC_ORIGIN` 必须精确等于浏览器实际访问的来源（含 scheme 与端口，无路径、无末尾斜杠）。
  POST 请求按此校验 `Origin`/`Referer`，不匹配即 403。
- 生产环境将 `DEV_PUBLIC_ORIGIN` 留空；非本地组合会在 API 启动时被拒绝。
- 顺序为先认证再校验来源：未登录的跨站 POST 返回 401 而不是 403。
- 不要为 `/uploads` 增加任何静态映射或反向代理：这些路径必须保持 404。
- 会话固定 24 小时过期，查询始终校验 `expires_at`。

---

## 8. 未知提交结果的孤立文件

正常回滚路径是确定的：

1. 解析失败或事务明确失败 → 回滚数据库写入，并尝试删除本次已保存的原文件。
2. 删除文件本身失败 → 请求仍返回失败，并记录一条待人工核对的错误（不含正文与秘密）。
3. **COMMIT 结果未知**（连接在提交确认前中断）→ 返回 500，**保留原文件**，
   并记录一条人工核对线索。

第 3 种情况下绝不自动删除文件：数据库可能已经提交成功，删文件会产生引用了不存在
文件的材料。人工处理原则是**先核对数据库再决定**：

```sql
SELECT id, class_id, stored_filename FROM materials WHERE stored_filename = '<线索中的文件名>';
```

- 查得到 → 材料已提交，保留文件即可。
- 查不到 → 该文件确为孤立文件，可安全删除 `uploads/<class_id>/<文件名>`。

崩溃或提交未知后的自动核对、24 小时定时扫描与上传状态机属于后续强化项，本迭代
不提供，也不作为交付门槛。

---

## 9. 目录结构

```text
backend/
  cmd/api/           进程入口：配置校验、解析器检查、迁移、seed、集合准备、补索引、HTTP 服务
  internal/
    auth/            登录、会话、班级隔离
    materials/       材料上传、解析、读取、下载
    chunking/        三种切分策略、参数校验、Unicode 字符区间
    retrieval/       向量库与网关客户端、索引生命周期、检索与问答、检索接口
    config/ db/ httpapi/ httpx/ seed/ health/   配置、连接、路由与共享契约
  migrations/        有序 SQL 迁移（编译进二进制）
  tests/             数据库集成套件 + 测试运行镜像
frontend/            React 18 + TypeScript + Vite 前端
deploy/nginx.conf    唯一的浏览器入口配置
tests/acceptance/    HTTP 验收入口（仅标准库，独立模块）
tests/browser/       浏览器端到端套件（无 npm 依赖的 DevTools 协议客户端）
tests/gateway/       验收用的确定性模型网关替身（不参与产品运行）
tests/evidence/      每次变更的可复核验收输出
compose.yaml         web / api / db / qdrant 四个服务
.env.example         空值配置模板
```

---

## 10. 本迭代不包含

流式长对话、重排序器（reranker）、OCR、跨班共享、客户端直连向量库或模型网关、
把向量分量暴露给浏览器、Agent、作业发布/提交/批改、用户注册、找回密码、
班级与用户管理后台、材料编辑与删除、旧版 `.doc`、`.docm`、文档密码解密、版式还原、
浏览器内 PDF/Word 预览，以及 DOCX 页眉页脚/批注/修订历史/文本框/嵌入对象解析。

检索侧的已知边界：ngram 分词固定为 2 字，单个汉字的关键字查询可能命中不到；
预处理后的字符区间属于预处理文本，不冒充原始 PDF/DOCX 页码或文件字节偏移。
