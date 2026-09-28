## Purpose

为认证与教学材料提供最小且一致的班级租户数据结构，通过数据库非空、唯一和关联约束阻止无班级归属或跨班关联的数据，并保证初始化可重复而不损坏已有教学材料和账号。

## ADDED Requirements

### Requirement: DATA-01 Minimal relational schema

MySQL 8 MUST 创建 classes(id,name,created_at)、users(id,username,password_hash,role,class_id,created_at)、sessions(id,session_id,user_id,created_at,expires_at)、materials(id,class_id,uploaded_by,title,original_filename,stored_filename,content_type,created_at)、knowledge_entries(id,class_id,material_id,content,created_at)。上述字段 MUST NOT NULL。users.username MUST UNIQUE；role MUST 仅为 teacher/student；sessions.session_id MUST 保存客户端随机 Session Token 的 SHA-256 摘要，编码为 64 个小写十六进制字符，并设置唯一约束；数据库 MUST NOT 保存原始 Session Token。users.class_id、materials.class_id、knowledge_entries.class_id/material_id MUST 有必要索引。MUST 使用外键保证班级存在、上传者与材料同班、知识条目与材料同班；每材料 MUST 一条知识记录。MUST NOT 为 Non-goals 建表。

#### Scenario: AC16 Materials requires class
- **GIVEN** 真实 MySQL 8 的迁移已完成且其他字段合法
- **WHEN** INSERT materials 设置 class_id=NULL
- **THEN** 数据库拒绝且无新行，省略 class_id 也失败

#### Scenario: AC17 Knowledge requires class
- **GIVEN** 有一份合法材料且其他字段合法
- **WHEN** INSERT knowledge_entries 设置 class_id=NULL
- **THEN** 数据库拒绝且无新行，省略 class_id 也失败

#### Scenario: DA01 Schema constraints
- **GIVEN** 全新测试数据库完成迁移
- **WHEN** 检查表结构、索引并分别插入重复用户名、非法 role、无班级 user、跨班上传者材料、跨班知识条目及重复 material_id 知识条目
- **THEN** 必需字段及索引存在，所有非法写入均被数据库拒绝；无未来功能业务表

### Requirement: DATA-02 Idempotent secret-safe seed

初始化 MUST 提供 Class A、Class B、teacher_a（Teacher A，teacher/Class A）、student_a1（Student A1，student/Class A）、teacher_b（Teacher B，teacher/Class B）、student_b1（Student B1，student/Class B）。四个初始密码 MUST 分别从 SEED_TEACHER_A_PASSWORD、SEED_STUDENT_A1_PASSWORD、SEED_TEACHER_B_PASSWORD、SEED_STUDENT_B1_PASSWORD 读取并生成 bcrypt hash；MUST NOT 使用默认密码。重复初始化 MUST NOT 重复创建班级/用户、重置已存在密码、清空材料或覆盖上传数据。缺少必须秘密 MUST 启动失败并给出不含秘密的提示。仓库 MUST 仅提供无真实值的 .env.example，并忽略 .env。

#### Scenario: AC20 Repeat initialization preserves data
- **GIVEN** 全新库已 seed，Teacher A 和 Teacher B 各已上传材料，记录账号/班级计数、hash、材料/知识条目及文件字节
- **WHEN** 再次执行 seed 并重启 Compose 两次
- **THEN** 仍只有两个种子班级和四个种子用户，四个原 hash、材料/知识条目及文件不变，可用原凭证登录

#### Scenario: DA02 Seed password source
- **GIVEN** 测试运行时生成的环境变量密码，仓库中没有这些值
- **WHEN** 初始化并读取四个种子用户的 password_hash，用各自环境变量密码做 bcrypt 校验；再移除 SEED_TEACHER_B_PASSWORD 单独尝试启动
- **THEN** 四个 hash 均不是明文且验证成功；缺少 SEED_TEACHER_B_PASSWORD 时启动失败，不落入默认密码

#### Scenario: AC29 Class B teacher seed login
- **GIVEN** 全新库已完成 seed，Teacher B 的用户名为 teacher_b，密码来自 SEED_TEACHER_B_PASSWORD
- **WHEN** 使用对应凭证 POST /api/login，再调用 GET /api/me
- **THEN** 登录返回 200 并设置 Session Cookie，me 返回 role=teacher、class_name=Class B，class_id 对应 Class B
