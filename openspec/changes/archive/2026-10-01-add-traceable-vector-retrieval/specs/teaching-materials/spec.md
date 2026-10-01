## MODIFIED Requirements

### Requirement: MAT-02 Safe supported uploads

POST /api/materials MUST 接受 multipart/form-data 的 title、唯一 file 及可选的切分策略字段，仅支持 .md/.txt/.pdf/.docx（后缀大小写归一），MUST NOT 信任客户端 Content-Type 或文件选择器。对于 .md/.txt，后端 MUST 验证完整有效 UTF-8、至少一个字节、无 NUL；SHOULD 检测明显二进制伪装，但 MUST NOT 仅因 PK、MZ、%PDF- 等短字面前缀拒绝合法普通文本。PDF/DOCX MUST 进行 MAT-05 的对应格式结构校验及文本提取，MUST NOT 把其二进制原文件按 UTF-8/NUL 文本规则拒绝。title MUST 为 trim 后 1..255 字符，文件 MUST <=5 MiB，请求 MUST <=6 MiB。非法字段/文本/危险文件名 MUST 400，超限 MUST 413，不支持扩展名 MUST 415，且不得产生材料或知识库记录。后端 MUST 阻止路径穿越、绝对路径与同名覆盖；磁盘文件名 MUST 由安全随机 UUID 与净化 basename 组成并在当前班级私有目录内排他创建。class_id、uploaded_by、role 等伪造表单字段 MUST 忽略，class_id/uploaded_by MUST 来自 Session 用户。成功 MUST 201 {material} 与 Location=/api/materials/{id}。可选切分字段为 chunk_strategy=auto|custom|hierarchy、chunk_max_chars、chunk_overlap_percent、chunk_separator、remove_urls_emails、fold_whitespace；仅 custom 使用长度、重叠、分隔符与预处理参数，auto 和 hierarchy 收到这些额外参数时 MUST 忽略。非法策略或超出 KR-01 范围的 custom 参数 MUST 400 且不得产生材料或知识正文；不传策略 MUST 使用 auto。索引工作在材料与知识正文提交后进行，嵌入失败 MUST NOT 撤销已成功提交的上传，索引状态按 KR-02 呈现。

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
#### Scenario: MA09 Upload strategy and invalid options
- **GIVEN** Teacher A 已登录且文件和标题合法
- **WHEN** 分别不传策略、传合法 custom 参数、传非法策略或越界 custom 参数
- **THEN** 前两次按 auto 或 custom 创建材料及知识正文并启动对应索引；非法选项返回 400 且无材料、知识正文或文件残留，原有文件安全校验和班级授权继续适用
