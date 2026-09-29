## Context

参见 proposal.md。当前 Go 服务已通过 Session 恢复用户和班级、在 MySQL 中保存 materials 与一条 knowledge_entries.content，并以同源 /api 提供材料列表、详情和下载。现有数据库尚无切片；Compose 只有 web/api/db，前端只有登录与材料页。课程要求在此基础上增加语义检索、Qdrant 与有据问答，同时不能放宽 MAT-01/MAT-03 的班级与上传事务边界。

## Goals / Non-Goals

**Goals:** 让检索结果可以由材料 ID、知识条目、切片序号、字符区间与关系库中的切片正文复核；三条检索路径使用同一会话班级；上传成功与索引成功分离；已有材料可补索引，重建后不残留旧切片；前端继续遵守 STYLE_GUIDE.md。

**Non-Goals:** 本次不实现流式长对话、重排序器、OCR、跨班共享、客户端直连向量库或网关，也不把向量分量暴露给浏览器。当前交付仅是规划文件，实施留待后续 apply。

## Decisions

1. **MySQL 保存正文，Qdrant 保存向量。** 新迁移将 knowledge_entries.content 安全更名为 body_text，增加 knowledge_chunks：id、class_id、material_id、knowledge_entry_id、chunk_index、chunk_text、start_offset、end_offset、offset_basis、index_status 和索引代次。每条切片与材料、知识条目保持同班外键约束；(knowledge_entry_id, index_generation, chunk_index) 唯一。Qdrant collection 为 campusclaw_chunks，余弦度量，向量维度从嵌入模型配置校验；point ID 使用与 MySQL 切片 ID 相同的 uint64，payload 仅含课程列出的五个标识。class_id 在 payload 中以十进制字符串精确匹配，避免 MySQL UNSIGNED BIGINT 超出向量库 JSON 有符号整数范围。为 class_id 建 payload 索引。相比把正文复制进 payload，回 MySQL 查询多一步，但出处只有一个权威文本来源；[Qdrant Points](https://qdrant.tech/documentation/concepts/points/) 支持 uint64 point ID；[Filtering](https://qdrant.tech/documentation/search/filtering/) 支持 payload 条件。

2. **切分采用可复现的策略配置。** auto 固定 800/80 并优先自然断点；custom 仅接受规定的三个分隔符、长度、重叠与预处理开关；hierarchy 保留 Markdown 标题，超长章节用 auto。索引代次保存策略与预处理参数。原文 body_text 不变；有预处理时 start/end 以预处理后文本的 Unicode 字符位置计算并标注 offset_basis=normalized，未预处理时为 extracted。可由 body_text 和所存配置重建待切分文本用于核验，不把其偏移误作原始 PDF/DOCX 页码或文件字节偏移。相比直接在原文件上标记，精确页码需额外解析映射，留待后续。

3. **关系库存状态，跨库写入按可重试流程完成。** 原文件、materials、knowledge_entries 仍依 MAT-03 同事务提交。提交后为材料建立持久 pending 索引任务并产生切片；嵌入每条切片，Qdrant upsert 成功后置 ready。失败置 failed、删除可能残留的点并保留材料原文，任务可重试。启动时对缺索引的历史条目按 auto 幂等补齐；同一材料重建以持久锁或代次隔离串行执行，先令旧代次不可检索，再删除旧 Qdrant 点和旧 MySQL 切片，按新策略生成。Qdrant 与 MySQL 无分布式事务，因此查询端始终核对 ready、代次与班级，且有补偿清理；不能仅凭 Qdrant 点存在就返回正文。相比在上传事务中调用外部模型，上传的成功结果不会被外部服务故障回滚。

4. **Go 服务统一执行检索与鉴权。** POST /api/search 接收 query、mode，POST /api/ask 接收最新 question 和可选有限历史；POST /api/materials/{id}/reindex 与 GET /api/materials/{id}/index 仅供本班教师。所有写接口沿用现有 Session、同源保护、JSON 错误封装及 no-store。请求中的 class_id 无论位于 body、query 或 header 都不参与过滤。keyword 走 MySQL FULLTEXT(chunk_text) WITH PARSER ngram，固定 ngram_token_size=2、只取本班 ready 行；vector 由服务端调用兼容 OpenAI /embeddings 的网关，然后 Qdrant 按 class_id 过滤、余弦分数至少 0.35，最后按 ID 加班级与代次回表；hybrid 两路分别过滤后按 1/(60+rank) 合并，缺席一路为零。[MySQL 8 ngram 文档](https://dev.mysql.com/doc/refman/8.0/en/fulltext-search-ngram.html)说明 2 字 token 的默认行为，这也是课程指定选型；单个汉字词可能无法独立命中 keyword，应由验收明确此边界。相比相加原始分数，RRF 不需要把全文相关度和余弦相似度强行归一。

5. **问答只读取已授权证据。** /api/ask 只用最新一句做混合检索，取前 4 条。空结果直接返回固定答案和空 citations，跳过对话网关。有命中时服务端生成 system 约束与 [1]…[4] 证据块，只传材料标题、序号、正文和本轮问题；有限历史放在其后，客户端 system 字段丢弃。响应前校验模型引用编号均指向已传入切片；无法核对的引用不作为出处呈现。对话网关仅负责撰写简短回答，不自行检索。与不检索直接问模型相比，这让答案能回到同班原文。

6. **部署与前端延续当前边界。** Compose 增加带持久卷的私有 Qdrant，只有 web 端口映射；API 持有嵌入和对话网关配置，浏览器仅使用相对 /api。前端在材料页增加检索和问答区域，教师上传与重建控件沿用共享视觉变量、窄屏和焦点规则。材料详情仍显示完整提取文本，引用入口复用既有班级授权 API。向量库失效时 keyword 单独运行，vector/hybrid/ask 不暗中改模式。

## Risks / Trade-offs

- **跨库短暂不一致** → 只有 ready 且属于当前索引代次、当前会话班级的 MySQL 行可作为命中；点写入失败标记 failed 并补偿删除；重启后扫描 pending/failed。
- **重建期间旧点或旧行被读到** → 先停用旧代次、清理旧点与旧行，按本次配置重建；同一材料串行，查询回表再验证代次。
- **模型输出伪造引用或照抄恶意提示** → 仅提供本班切片、由服务端编写 system 指令并校验引用编号；无证据时完全不调用对话模型。
- **预处理后位置不能直接定位原文件字节或 PDF 页码** → 响应明确标注偏移基准，提供切片正文与授权材料详情，不宣称页码精确定位。
- **嵌入模型维度变更** → 启动时核对集合维度；变更模型须新集合或全量重建，不能混写不同维度向量。
- **外部网关延迟或成本** → keyword 不调用网关；向量与问答限定候选和超时，网关异常返回通用可重试错误，秘密只在服务端。

## Migration Plan

1. 先备份 MySQL 和上传卷；添加切片表、索引任务/代次状态及 ngram 全文索引。将原 content 列更名为 body_text，更新 Go 读取路径但保持现有材料 API JSON 形状。
2. 为 Compose 增加 Qdrant 私有卷、集合初始化及服务端网关配置；确认向量维度、余弦度量和只有 web 暴露端口。
3. 部署 API/前端后幂等扫描历史 knowledge_entries，以 auto 补索引；保留原文件与完整正文。索引失败保留 failed 状态供教师重试，不将其伪装为上传失败。
4. 用 A/B 班资料、同义改写、无依据问句和故障注入验收三模式、跨班隔离、出处、问答及回滚边界。若需回退应用版本，停用新检索入口并保留新增表与 Qdrant 卷以备排查；数据库列更名须在旧版 API 启动前执行逆迁移，不能直接混跑新旧二进制。
