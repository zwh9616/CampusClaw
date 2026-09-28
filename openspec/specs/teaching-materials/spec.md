# teaching-materials Specification

## Purpose
TBD - created by archiving change add-auth-class-materials. Update Purpose after archive.

## Requirements

### Requirement: MAT-01 Class-scoped list and detail

GET /api/materials MUST 返回 200 {materials:[...]}，仅包含当前 Session 用户 class_id 的数据；空列表 MUST 为 []。GET /api/materials/{id} MUST 返回 200 {material,content}，content 是该材料的提取文本（.md/.txt 为原文，PDF/DOCX 为 MAT-05 规定范围的解析结果）。详情查询 MUST 同时限定 id 与 current_user.class_id，知识文本读取 MUST 同时限定 material_id 与 class_id；MUST NOT 仅凭 ID 查询。跨班、不存在及无效 ID MUST 统一 404 not_found 相同错误体；{id} MUST 仅含 ASCII 十进制数字且值在 BIGINT UNSIGNED 的 1..18446744073709551615 范围，允许前导零；非数字、空格、符号、0、负数、小数、科学计数及溢出 MUST 返回同样 404；未登录 MUST 401。所有班级范围 MUST 从服务端身份恢复，客户端 query/path/body MUST NOT 指定或改变范围；query 中 class_id 固定忽略。返回元数据 MUST 包括 id,class_id,uploaded_by,title,original_filename,content_type,created_at，MUST NOT 返回存储磁盘路径。ID MUST 序列化为十进制字符串。

#### Scenario: AC09 Same-class visibility after upload
- **GIVEN** Teacher A 成功上传材料并取得 201
- **WHEN** Student A1 立即 GET /api/materials，并 GET /api/materials/{新id}
- **THEN** 列表包含新材料，详情 200；.md/.txt 内容与原文一致，PDF/DOCX 与预期提取文本一致

#### Scenario: AC10 Other-class list
- **GIVEN** Class A 存在材料且 Student B1 登录
- **WHEN** GET /api/materials 并再次请求 ?class_id=ClassA的id
- **THEN** 两次列表均不包含任何 Class A 材料，伪造参数不能改变班级范围

#### Scenario: AC11 Other-class detail
- **GIVEN** Student B1 知道 Class A 的材料 ID
- **WHEN** 请求该 ID 的详情，并请求不存在 ID 的详情
- **THEN** 两者均 404 且错误体、内容类型一致，不透露材料是否存在

### Requirement: MAT-02 Safe supported uploads

POST /api/materials MUST 接受 multipart/form-data 的 title 和唯一 file，仅支持 .md/.txt/.pdf/.docx（后缀大小写归一），MUST NOT 信任客户端 Content-Type 或文件选择器。对于 .md/.txt，后端 MUST 验证完整有效 UTF-8、至少一个字节、无 NUL；SHOULD 检测明显二进制伪装，但 MUST NOT 仅因 PK、MZ、%PDF- 等短字面前缀拒绝合法普通文本。PDF/DOCX MUST 进行 MAT-05 的对应格式结构校验及文本提取，MUST NOT 把其二进制原文件按 UTF-8/NUL 文本规则拒绝。title MUST 为 trim 后 1..255 字符，文件 MUST <=5 MiB，请求 MUST <=6 MiB。非法字段/文本/危险文件名 MUST 400，超限 MUST 413，不支持扩展名 MUST 415，且不得产生材料或知识库记录。后端 MUST 阻止路径穿越、绝对路径与同名覆盖；磁盘文件名 MUST 由安全随机 UUID 与净化 basename 组成并在当前班级私有目录内排他创建。class_id、uploaded_by、role 等伪造表单字段 MUST 忽略，class_id/uploaded_by MUST 来自 Session 用户。成功 MUST 201 {material} 与 Location=/api/materials/{id}。

#### Scenario: AC06 Markdown upload
- **GIVEN** Teacher A 已登录且提交有效 title 和 UTF-8 .md 文件
- **WHEN** POST /api/materials
- **THEN** 201，原文件字节保存，materials 和 knowledge_entries 各增加一条且属于 Class A，uploaded_by=Teacher A，知识内容为完整原文

#### Scenario: AC07 Text upload
- **GIVEN** Teacher A 已登录且提交有效 title 和 UTF-8 .txt 文件
- **WHEN** POST /api/materials
- **THEN** 201，原文件、材料和完整知识文本均保存且归属 Class A

#### Scenario: AC08 Unsupported extension leaves no records
- **GIVEN** Teacher A 已登录，记录数据库及私有文件数量
- **WHEN** 上传 .doc、.docm、.zip 或 .exe（逐一测试）并伪装 text/plain
- **THEN** 各返回 415，materials/knowledge_entries 行数和文件数量不增加

#### Scenario: AC15 Forged tenant on upload
- **GIVEN** Teacher A 的 Class A Session
- **WHEN** 上传合法 .md 并在表单/query 中提交 Class B 的 class_id、Student B1 的 uploaded_by
- **THEN** 返回 201，但材料、知识条目、磁盘目录均属于 Class A，uploaded_by=Teacher A，Class B 无新增记录

#### Scenario: AC30 Class B teacher uploads for own class
- **GIVEN** Teacher B 以 teacher_b 的 Class B Session 登录，Student B1 和 Student A1 分别有各自班级的 Session
- **WHEN** Teacher B 上传合法 .md 材料；两名学生分别请求列表、该材料详情与原文件下载
- **THEN** 上传返回 201，materials、knowledge_entries 和私有文件均归属 Class B，uploaded_by=Teacher B；Student B1 可列出、查看和下载且下载字节与原文件一致；Student A1 列表不可见、详情和下载均返回相同的 404 not_found

#### Scenario: MA01 Text and size validation
- **GIVEN** Teacher A 的 Session
- **WHEN** 分别上传 .txt 包装的二进制、无效 UTF-8、NUL、空文件、超过 5 MiB 文件，或缺失 title/file、多个 file
- **THEN** 非法内容/字段 400、超限 413，均无数据库行和残留文件

#### Scenario: MA02 Path traversal and duplicate names
- **GIVEN** Teacher A 的 Session
- **WHEN** 分别提交原始 filename 为 ../x.txt、反斜杠穿越路径、绝对 Windows/Linux 路径，再两次上传正常同名文件
- **THEN** 危险路径返回 400 且不能写到班级目录外；正常同名上传均 201，生成两个不同存储名，旧文件字节不变

### Requirement: MAT-03 Atomic material and knowledge persistence

一次成功上传 MUST 保存原文件、materials 和一条提取文本 knowledge_entries，后两者 MUST 在同一数据库事务中提交。knowledge_entries 写入失败 MUST 回滚 materials，返回 500，已保存文件 MUST 尝试删除。MUST NOT 返回成功或留下仅有 materials 的脏数据。文件保存/解析失败 MUST NOT 开始材料写入事务；解析失败 MUST 尝试删除原文件。删除失败 MUST 记录待人工核对的错误，MUST NOT 把失败报告为成功。COMMIT 结果未知 MUST 返回 500、保留原文件并记录核对线索，MUST NOT 盲删可能已提交的文件。崩溃或未知提交遗留文件 SHOULD 在后续强化迭代增加自动恢复；本迭代 MUST NOT 将自动核对、24 小时扫描/定时清理作为交付条件。

#### Scenario: AC18 Knowledge insert failure rollback
- **GIVEN** Teacher A 登录且测试环境仅对 knowledge_entries INSERT 注入失败，材料 INSERT 可成功
- **WHEN** 上传合法文件
- **THEN** 返回 500；独立数据库连接查询两张表均无本次新增行；正常可写文件系统中本次文件已删除

#### Scenario: MA03 Minimal failure compensation
- **GIVEN** 测试可注入文件写失败、明确回滚后的文件删除失败、COMMIT 确认丢失
- **WHEN** 分别触发三种错误
- **THEN** 写失败无数据库新增行；删除失败的请求仍为失败并有核对日志；未知 COMMIT 返回 500 并保留原文件，无盲删或虚报成功，不要求自动扫描器

### Requirement: MAT-04 Authorized file download

GET /api/materials/{id}/file MUST 按认证 → 根据 current_user.class_id 的班级授权查询 → 读取文件执行，MUST NOT 先打开文件再鉴权。本班原文件存在返回 200 原始字节，Content-Disposition MUST attachment，MUST 设置 nosniff/no-store；跨班、不存在、无效 ID 或文件缺失 MUST 返回同一 404 not_found；无效 ID 的定义 MUST 与 MAT-01 完全一致，未登录 MUST 401。原始文件 MUST NOT 经 /uploads 静态公开。

#### Scenario: AC12 Other-class download
- **GIVEN** Student B1 知道 Class A 的材料 ID
- **WHEN** GET /api/materials/{id}/file 并与不存在 ID 比较
- **THEN** 均为相同 404 响应且无任何文件字节

#### Scenario: AC13 Same-class download
- **GIVEN** Student A1 登录，Teacher A 已上传 Class A 的 .md/.txt/.pdf/.docx 材料（逐一测试）
- **WHEN** GET /api/materials/{id}/file
- **THEN** 200，下载字节与原始上传完全一致，响应包含安全附件文件名及 nosniff/no-store

#### Scenario: MA04 Teacher reads own class
- **GIVEN** Teacher A 登录且本班已有材料
- **WHEN** 调用列表、详情、下载三个 GET API
- **THEN** 均 200，本班材料可见，详情的原文或提取文本与预期一致，下载原文件字节正确

#### Scenario: MA05 Identical invalid-ID handling
- **GIVEN** Student A1 有效 Session
- **WHEN** 对详情和下载分别请求 abc、0、-1、+1、1.5、1e3、带空格数字、18446744073709551616，并与不存在的合法 ID 比较
- **THEN** 全部同样 404 not_found、相同错误体；未登录进行这些请求全部 401；前导零形式 001 与 1 的授权结果相同

### Requirement: MAT-05 PDF and DOCX text extraction

PDF/DOCX 上传 MUST 同步完成格式校验和文本提取后才写入材料/知识条目事务。PDF MUST 提取可读取文本层；DOCX MUST 按正文顺序提取段落与表格中的文本、换行和制表符，支持常见 Transitional/Strict WordprocessingML 包；MUST NOT 把任意 ZIP 当作 DOCX。MUST 保留原始文件供下载，MUST 将提取文本写入一条 knowledge_entries，并在详情返回相同文本。MUST NOT 执行宏、脚本、嵌入对象、外部链接抓取或 OCR。DOCX 页眉页脚/批注/修订历史/文本框/嵌入对象解析及版式还原不在支持范围。

PDF/DOCX 提取结果 MUST 为有效 UTF-8、无 NUL、包含非空白文本、使用 LF 换行。错误固定为：不支持扩展名 415；格式伪装/结构损坏 400；加密 PDF、以 .docx 上传的 OLE/CFB 容器（包括加密 Office 包）、扫描型/无正文可提取文档或解析超时 422；容量限制超限 413；解析器不可用/内部故障 500。MUST NOT 返回解析器 stderr/磁盘路径。解析失败 MUST 无新增 materials/knowledge_entries 并尝试删除本次原文件；成功 201 后同班学生立即可查看文本和下载原文件。

解析 MUST 有资源上限：PDF <=200 页，DOCX <=1000 ZIP entries、声明及实际读取累计解压字节 <=20 MiB、XML 深度 <=128；所有文档提取文本 <=5 MiB、解析时间 <=10 秒。容量超限 MUST 413、超时 MUST 422 且终止解析并释放资源。DOCX MUST 拒绝 ZIP 路径穿越/重复 part、损坏 CRC/XML、DOCTYPE/自定义实体、宏 part、外部主文档关系；普通外链超链接仅保留显示文本，不访问目标。PDF/DOCX 解析 MUST 至多并发一个，等待槽位超过 2 秒返回 503 busy，不创建业务行并尝试删除本次文件。

#### Scenario: AC25 PDF upload and extracted text
- **GIVEN** Teacher A 已登录，合法未加密 PDF 包含可提取的中英文文本，文件/页数在限额内
- **WHEN** 上传该 PDF，Student A1 随后查询列表、详情和下载
- **THEN** 上传 201，一份 materials 和一份知识文本均属 Class A；列表可见，详情包含预期中英文文本；下载字节与原 PDF 完全相同，content_type=application/pdf

#### Scenario: AC26 DOCX upload and extracted text
- **GIVEN** Teacher A 已登录，合法无宏 DOCX 含中英文段落和表格，分别提供 Transitional/Strict fixtures
- **WHEN** 上传并让 Student A1 查看详情和下载
- **THEN** 各上传 201，知识文本保留段落顺序、单元格文字和分隔符，详情等于知识文本，下载字节等于原 DOCX，类型为 application/vnd.openxmlformats-officedocument.wordprocessingml.document

#### Scenario: AC27 Document validation failures are atomic
- **GIVEN** Teacher A 登录且两表/文件数量已记录
- **WHEN** 分别上传损坏 PDF、伪装为 DOCX 的普通 ZIP、损坏 DOCX、加密 PDF/Word、纯扫描 PDF、仅图片 DOCX
- **THEN** 损坏或伪装 400，加密/无可提取文本 422；所有请求无新增两表记录，正常文件系统中本次原文件已清理

#### Scenario: AC28 New formats retain all access controls
- **GIVEN** Teacher A 上传 PDF/DOCX 时伪造 Class B 的 class_id 和 Student B1 的 uploaded_by
- **WHEN** Student A1 与 Student B1 分别列出/查看/下载这些材料，学生同时直接尝试上传两种格式，未登录调用受保护接口，并直接请求实际 /uploads 路径
- **THEN** 成功材料仍属于 Class A/Teacher A；A1 可读可下载；B1 列表不可见、详情/下载同样 404；学生上传 403；未登录 API 401；所有直接 /uploads 请求 404

#### Scenario: MA06 Literal text prefixes remain valid
- **GIVEN** 合法 UTF-8 .md/.txt 正文分别以 PK、MZ、%PDF- 开头，不含 NUL
- **WHEN** Teacher A 上传文件
- **THEN** 201，知识内容与原文一致，不因短前缀误判为 ZIP/可执行文件/PDF

#### Scenario: MA07 Parser bounds and hostile documents
- **GIVEN** 可控 fixtures 分别超过页数、entries、解压字节、输出字节或 XML 深度，并提供 ZIP 路径穿越/重复 part/DOCTYPE/宏/外部主文档关系
- **WHEN** Teacher A 上传，另以测试注入令解析超过 10 秒或并发槽位等待超过 2 秒
- **THEN** 容量超限 413、结构/安全违规 400、解析超时 422、槽位超时 503；无两表新增行，原文件补偿清理；无包外写入/外部网络请求，解析资源释放

#### Scenario: MA08 Knowledge failure after document extraction
- **GIVEN** PDF/DOCX 可正常解析且测试仅令 knowledge_entries INSERT 失败
- **WHEN** Teacher A 上传两种格式（独立执行）
- **THEN** 均 500，materials INSERT 回滚、无知识记录，正常文件系统中已保存原文件删除
