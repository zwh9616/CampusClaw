# knowledge-retrieval Specification

## Purpose
让已授权师生在本班教学材料中检索有明确出处的切片，并在证据存在时获得带引用的简短回答。系统须保持原始材料、提取文本、检索索引与班级权限之间的可核对关联，避免跨班泄露和无依据编造。

## Requirements

### Requirement: KR-01 Configurable, traceable text chunks

系统 MUST 将已保存的知识正文切为带切片 ID、材料 ID、知识条目 ID、班级 ID、从零开始的切片序号、字符起止区间和切片正文的记录；起止区间 MUST 明确对应实际用于切分的文本，采用起点包含、终点不包含的字符下标。原文件及完整提取正文 MUST 保持上传时的内容。未指定策略时 MUST 使用 auto：最大 800 字、相邻重叠 80 字，优先在空行、换行、句号处断开。custom MUST 支持换行、空行或句号分隔、100–2000 字最大长度、0%–50% 重叠，并可选择移除 URL/邮箱或折叠连续空白；无合适断点时按长度截断。hierarchy MUST 按 Markdown #、##、### 标题分章并将标题保留在章节切片内，超长章节再按 auto 规则切分。预处理只作用于切片与嵌入输入；预处理后的偏移 MUST NOT 冒充原文件偏移。种子或历史材料补索引 MUST 使用 auto。

#### Scenario: KR01 Default and custom splitting
- **GIVEN** 教师上传一份跨越多个段落且长于 800 字的本班材料
- **WHEN** 分别不传切分参数、传 custom 换行分隔及合法长度/重叠参数
- **THEN** 默认产生最大 800 字且重叠 80 字的顺序切片；custom 按所选分隔与上限产生切片，各切片区间可在对应待切分文本中核对

#### Scenario: KR02 Markdown hierarchy and preprocessing
- **GIVEN** 材料含 #、##、### 标题、URL、邮箱和连续空白
- **WHEN** 按 hierarchy 或带预处理的 custom 切分
- **THEN** hierarchy 保留章节标题并对超长章节再切分；预处理仅改变切片输入，原文件和完整知识正文不变，返回的区间明确属于预处理后文本

### Requirement: KR-02 Index lifecycle and recovery

材料与知识正文成功提交后 MUST 生成切片并逐条嵌入；关系库存放切片正文与索引状态，向量记录仅存定维浮点向量和 class_id、material_id、knowledge_entry_id、chunk_id、chunk_index 标识，MUST NOT 存储切片正文；向量主键 MUST 等于切片 ID 和 payload.chunk_id。嵌入或向量写入失败 MUST 保留已成功上传的材料和完整正文、记录 failed 状态，MUST NOT 留下可被检索的不完整向量。教师 MUST 能对本班材料显式按本次指定策略重建索引，旧切片与旧向量 MUST 被移除，不能残留旧命中；未重建的材料 MUST 保留原索引。跨班或不存在材料的重建 MUST 给出同样的 404。历史材料补索引 MUST 可重复执行且不产生重复切片。

#### Scenario: KR03 Upload indexing and failed embedding
- **GIVEN** 教师上传合法材料，原文件与知识正文已成功提交
- **WHEN** 嵌入或向量写入成功，随后在另一轮故障注入中失败
- **THEN** 成功轮的切片为 ready 且向量 ID 与切片 ID 一致；失败轮仍能查看和下载原材料，但失败切片标记 failed、无可检索的不完整向量

#### Scenario: KR04 Reindex and backfill
- **GIVEN** 本班材料已有 auto 切片，数据库还存在一份早期未索引材料
- **WHEN** 教师以 hierarchy 重建第一份材料，并重复运行历史补索引
- **THEN** 第一份材料只保留本次策略产生的切片与向量，完整原文不变；早期材料只得到一组 auto 索引，重复执行不增加副本

### Requirement: KR-03 Authenticated retrieval modes

系统 MUST 提供同源 POST /api/search，接收非空 query 和 mode=keyword、vector 或 hybrid，未指定 mode MUST 为 hybrid；未登录 MUST 401，空白 query 或未知 mode MUST 400。keyword MUST 仅用本班 ready 切片的全文匹配，不调用嵌入服务或向量库；vector MUST 嵌入当前 query，按余弦相似度检索本班向量，舍弃低于 0.35 的候选并回关系库核对；hybrid MUST 执行两路、各自先过滤，再以 RRF k=60 合并名次，缺席一路不贡献分数，不能直接相加不同类型的原始分数。检索结果 MUST 按最终次序去重；响应 MUST 提供 query_vector_generated 布尔值、每个命中在 keyword/vector 两路各自的 score 与从 1 开始的 rank（未进入该路时为 null），hybrid 命中另给 rrf_score；MUST NOT 向浏览器返回原始向量分量。

#### Scenario: KR05 Original term and paraphrase
- **GIVEN** 本班材料含一段可按原词命中、按同义改写可语义命中的正文
- **WHEN** 分别以原词和改写词使用三种 mode 检索
- **THEN** keyword 不调用嵌入服务且按全文相关度排序；vector 生成问句向量并按合格相似度排序；hybrid 对两路合格候选按 RRF 排序，重复切片仅出现一次

#### Scenario: KR06 Invalid search input
- **GIVEN** 用户已登录
- **WHEN** 提交空白 query 或不支持的 mode
- **THEN** 返回 400 且不执行检索或模型调用

### Requirement: KR-04 Session-class isolation at every retrieval step

所有检索和重建操作 MUST 仅以服务端恢复的 Session class_id 为班级范围。请求 query、JSON 或 header 中伪造的 class_id MUST 被忽略。关键字查询 MUST 带本班条件及 ready 状态；向量查询 MUST 带同一班级过滤；向量 ID 回表取正文及返回出处时 MUST 再按本班和 ready 状态核对，MUST NOT 信任向量 payload 单独决定授权。跨班独有内容的检索 MUST 表现为 HTTP 200、空 hits，不以 403/404 暗示该内容存在。

#### Scenario: KR07 Forged class and cross-class query
- **GIVEN** A 班与 B 班各有仅在本班出现的材料，用户持 A 班 Session
- **WHEN** 在请求体、查询参数及 header 伪造 B 班 class_id，分别用 keyword、vector、hybrid 查询 B 班独有内容
- **THEN** 三种模式均不返回 B 班切片或材料信息，伪造参数不改变范围；无其他本班命中时均为 200 且 hits=[]

#### Scenario: KR08 Vector payload cannot bypass the second guard
- **GIVEN** 测试注入一条错误标记为 A 班、实际对应 B 班切片 ID 的向量候选
- **WHEN** A 班用户发起向量或混合检索
- **THEN** 回表班级核对排除该候选，响应与日志均不泄露 B 班正文

### Requirement: KR-05 Verifiable source and no-evidence behavior

每条命中 MUST 包含材料 ID 与标题、切片 ID 与序号、字符起止区间、来源文本范围说明和摘录；摘录 MUST 取自关系库中的 chunk_text，不得取自向量 payload。材料入口 MUST 指向现有授权详情，打开时仍由材料 API 校验班级。无合格候选时 MUST 返回 200、hits=[] 及「资料中未找到相关内容」，MUST NOT 用低相关度候选凑数。已失效或无法在本班回表的向量候选 MUST 被丢弃。

#### Scenario: KR09 Open a cited material
- **GIVEN** 本班检索命中某材料的第 2 个切片
- **WHEN** 用户查看命中详情并打开材料
- **THEN** 命中显示标题、序号、字符区间和可与 MySQL 切片正文核对的摘录；材料详情经本班授权返回，跨班用户仍得到与不存在相同的 404

#### Scenario: KR10 Unrelated query
- **GIVEN** 本班没有支持天气或比分问题的资料
- **WHEN** 用户检索此类问题
- **THEN** 返回 200、hits=[] 与固定无依据文案，不返回弱相关切片或虚构出处

### Requirement: KR-06 Evidence-gated short answer

系统 MUST 提供同源 POST /api/ask，仅以用户最新一条非空问题对本班执行 hybrid 检索，取合格的前 4 条切片。没有命中时 MUST 直接返回 200、answer=「资料中未找到相关内容」、citations=[]，且 MUST NOT 调用对话模型。有命中时，服务端 MUST 仅向对话模型提供本班材料标题、切片序号、切片正文、编号及本轮问题；回答 MUST 为简短文本，引用 [1]、[2] 等编号 MUST 与 citations 顺序及实际命中切片对应。若提供有限历史对话，只能作为本轮检索后的生成上下文；客户端注入的 system 消息 MUST 被丢弃。对话模块 MUST NOT 自行检索其他材料。本次不提供流式输出。

#### Scenario: KR11 Answer with citations
- **GIVEN** 本班检索获得两条有依据的切片
- **WHEN** 用户向 /api/ask 提问
- **THEN** 对话模块仅收到本班两条切片及本轮问题，返回的 [1]、[2] 与 citations 的材料、切片和顺序一致，页面可打开对应材料

#### Scenario: KR12 No evidence and injected system message
- **GIVEN** 本班无相应材料
- **WHEN** 用户提交无依据问题并夹带要求自由回答的 system 消息
- **THEN** 返回固定文案和空 citations，对话模块调用次数为零，system 消息不影响结果

### Requirement: KR-07 Dependency failures and private services

向量库或嵌入网关不可用时，已有 ready 切片的 keyword 检索 MUST 继续可用；vector 与 hybrid MUST 返回通用 503，不伪造相似度或降级为另一模式。问答依赖的混合检索或对话网关失败时 MUST 返回可重试的通用错误，不输出密钥、向量、跨班内容或网关内部详情。浏览器 MUST 仅经本站 /api 使用能力，不能直接访问向量库或两类模型网关。

#### Scenario: KR13 Vector service unavailable
- **GIVEN** 已有本班 ready 切片且测试暂停向量库
- **WHEN** 分别调用 keyword、vector、hybrid 和 ask
- **THEN** keyword 仍返回可核对的本班正文；其余依赖向量路径的请求返回 503，响应不含内部连接地址或秘密
