# 可追溯知识库检索与问答：验收记录（2026-09-30）

对应变更：`openspec/changes/add-traceable-vector-retrieval`（KR-01–KR-07、WEB-04/05、RUN-06、MAT-02）。

## 1. 环境

- Compose 项目 `campusclaw-zwh-acc`（隔离于日常开发栈，独立数据卷），web 发布在 8090。
  运行方式见第 6 节；日常栈仍占用 8080，两者互不影响。
- 向量库：compose 中的私有 `qdrant`（`qdrant/qdrant:v1.19.1`），集合 `campusclaw_chunks`，1536 维，Cosine。
- 模型网关：`tests/gateway`（本仓库内的确定性替身，仅用于验收）。
  本机没有可用的嵌入/对话服务，`tests/gateway` 用「概念词袋」产生向量，
  因此「同义改写能被语义路径命中、而关键字路径命中不了」可以在没有真实模型的情况下被真实地验证。

## 2. Go 单元与集成测试

```
$ docker run --rm --network campusclaw_zwh_test \
    -e MYSQL_HOST=campusclaw_zwh_test_db ... -v <repo>/backend:/src -w /src \
    campusclaw-zwh-test:latest sh -c "go test -count=1 -timeout 900s ./..."
ok  campusclaw/internal/auth      0.009s
ok  campusclaw/internal/chunking  0.007s
ok  campusclaw/internal/config    0.010s
ok  campusclaw/internal/httpapi   0.007s
ok  campusclaw/internal/httpx     0.007s
ok  campusclaw/internal/materials 0.032s
ok  campusclaw/tests              141.389s
```

新增覆盖：切片三策略与参数边界（`internal/chunking`）、维度校验与密钥不外泄（`internal/config`）、
迁移后旧材料详情可读与同班外键（`tests/chunks_schema_test.go`）、
索引生命周期与故障补偿、三种检索、跨班隔离、伪造 payload、RRF、问答引用（`tests/retrieval_test.go`）。

## 3. HTTP 端到端验收（经 Nginx，与浏览器同一入口）

```
34 checks, 0 failed
```

逐项：

```
AC21 PASS  /health 无需登录        AC29 PASS  Teacher B 身份
AU07 PASS  链路无 Cookie            AC04 PASS  未登录 401
AC03 PASS  统一 401                 AC01/AC02 PASS 身份与班级
AC05 PASS  学生上传 403             AC08 PASS  不支持类型 415
AC-MA01/02 PASS 校验与危险文件名    AC06/AC07/AC09 PASS 上传与即时可见
AC10 PASS  B 班看不到 A 班材料      AC11/AC12 PASS 跨班与不存在同为 404
AC30 PASS  B 班材料留在本班         AC13 PASS  下载字节一致
AC-MA04 PASS 教师可读本班           AC14 PASS  /uploads 一律 404
AC15 PASS  伪造租户无效             AC19 PASS  登出后会话失效
AC25/AC26/AC27/AC28 PASS PDF 与 DOCX
KR01 PASS  上传切分策略生效、索引就绪、非法策略 400 且不留痕迹
KR02 PASS  三种检索的本班命中与可核对出处
KR03 PASS  无依据查询 200 空命中与固定文案
KR04 PASS  伪造 class_id 不改变检索范围（A 班查不到 B 班内容）
KR05 PASS  有据问答的引用与无依据固定回答
KR06 PASS  教师重建成功、学生 403、跨班与不存在同为 404
AC31 PASS  登录限流 429
```

同一套用例在**日常开发库**上运行时 AC10 会失败（`Student B1 saw 1 materials, want 0`），
原因是该库残留了先前验收运行创建的 B 班材料（`created_at` 早于本次运行），并非本次改动的回归；
在干净数据卷上 AC10 通过。上面的结果来自干净数据卷。

## 4. 依赖故障（KR-07 / RUN-06）

`docker stop campusclaw-zwh-acc-qdrant-1` 前后，同一组请求：

| 请求 | 依赖正常 | 停止 Qdrant 后 |
| --- | --- | --- |
| `keyword` 检索 | 200 命中 | **200 命中**（不依赖向量库） |
| `vector` 检索 | 200 命中 | **503** `service_unavailable` |
| `hybrid` 检索 | 200 命中 | **503** `service_unavailable` |
| `/api/ask` | 200 回答 | **503** `service_unavailable` |
| 材料详情 | 200 | **200** |
| 材料下载 | 200 原文件 | **200 原文件** |

503 响应体固定为 `{"error":{"code":"service_unavailable","message":"服务暂不可用"}}`，
不含连接地址、密钥、向量或跨班内容。

## 5. 浏览器端到端（真实 Chrome）

```
11 checks, 0 failed
```

```
WE02/WE01 PASS  登录、刷新恢复、全程无 Cookie
WE03 PASS  教师上传、详情显示提取文本、下载走认证接口
WE04 PASS  学生无上传入口、localStorage 篡改无效、材料内脚本不执行
WE13 PASS  教师可见切分策略与索引状态，按 hierarchy 重建后仍可检索
WE10 PASS  学生三种方式检索、出处可打开、空输入提示、无依据固定文案
WE12 PASS  回答编号与出处一一对应，点选 [1] 选中第一条出处
WE11 PASS  390px 视口下检索可用且无横向滚动
WE05/WE06 PASS 登出与登录 429
AC23 PASS  浏览器只访问发布来源
```

## 6. 复现方式

```bash
# 1) 模型网关替身（仅验收需要；产品本身连真实网关）
docker build -t campusclaw-zwh-gateway:latest tests/gateway
docker run -d --name campusclaw-zwh-stub-gateway -p 127.0.0.1:11434:8080 campusclaw-zwh-gateway:latest

# 2) 隔离的验收栈（独立数据卷，web 发布在 8090）
DEV_PUBLIC_ORIGIN= PUBLIC_ORIGIN=http://localhost:8090 \
docker compose -p campusclaw-zwh-acc \
  -f compose.yaml -f tests/acceptance/override-8090.yaml up -d --build

# 3) HTTP 验收
docker run --rm --network campusclaw-zwh-acc_default -v "$PWD/tests/acceptance:/src" -w /src \
  -e ACCEPTANCE_BASE_URL=http://web:80 \
  -e ACCEPTANCE_TEACHER_A_PASSWORD=... -e ACCEPTANCE_STUDENT_A1_PASSWORD=... \
  -e ACCEPTANCE_TEACHER_B_PASSWORD=... -e ACCEPTANCE_STUDENT_B1_PASSWORD=... \
  golang:1.25-alpine sh -c "go env -w GOPROXY='https://goproxy.cn|https://proxy.golang.org' && go run ."

# 4) 浏览器验收（本机需要 Chrome）
cd tests/browser
CHROME_PATH="C:/Program Files/Google/Chrome/Application/chrome.exe" \
BROWSER_BASE_URL=http://localhost:8090 TEACHER_A_PASSWORD=... ... node check.mjs
```

`.env` 中的 `EMBEDDING_BASE_URL` / `CHAT_BASE_URL` 指向 `http://host.docker.internal:11434/v1`
即可接上替身；换成真实网关地址与模型名即可接上真实服务，代码不需要改动。

## 7. 其他检查

- `openspec validate --changes --strict`：`add-traceable-vector-retrieval` 通过。
  （`add-auth-class-materials` 仍报 “Change must have at least one delta”，属于此前已归档变更的历史状态，与本次无关。）
- `git diff --check`：无空白错误。
- 前端 `npm run typecheck`、`npm test`（22 项）、`npm run build` 均通过。

## 8. 未覆盖与已知边界

- 嵌入模型为替身，只验证了「向量通路、阈值、班级过滤、回表核对、RRF、引用校验」这些
  本系统负责的行为；真实模型的语义质量不在本次验证范围。
- ngram 分词固定为 2 字，单个汉字的关键字查询可能命中不到，这是课程指定选型的已知边界。
- 预处理后的字符区间属于预处理文本（`offset_basis=normalized`），不冒充原始 PDF/DOCX 页码或文件字节偏移。
