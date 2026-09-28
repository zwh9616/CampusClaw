## Why

CampusClaw 当前仓库只有 OpenSpec 初始化结构，尚无认证、班级隔离和教学材料业务实现。需要先建立可信身份和班级租户边界，让教师安全发布材料、同班学生查看下载，并将原文或文档提取文本写入知识库，为后续迭代提供可靠数据基础。

## What Changes

- 建立 React 18 + TypeScript + Vite、Go net/http、MySQL 8、Nginx、Docker Compose 的前后端分离基础；浏览器仅通过 Nginx 的 8080 端口访问。
- 实现 bcrypt 密码登录、服务端 Session、当前用户查询和登出，仅支持 teacher/student。
- Go 后端独立实施教师上传权限和基于 Session 用户 class_id 的班级隔离；跨班详情、下载统一 404。
- 支持 .md/.txt/.pdf/.docx 安全上传、原文件私有存储；提取文本并在同一事务中写入 materials 与 knowledge_entries，提供本班列表、解析文本查看和原文件下载。PDF 仅提取可读取的文本层，DOCX 提取正文段落和表格文本。
- 提供两班三账号的幂等环境变量 seed、React 登录及材料页面、健康检查和可重复验收。

## Capabilities

### New Capabilities

- `identity-session`: bcrypt、服务端 Session、登录/登出/me、认证与上传角色权限。
- `class-data`: 最小 MySQL 数据结构、班级约束与幂等 seed。
- `teaching-materials`: 租户隔离的列表/详情/下载、安全上传与知识库事务。
- `web-experience`: React 登录恢复、材料展示与教师上传交互。
- `compose-runtime`: Nginx 路由、私有存储、Compose 启动及 health。

### Modified Capabilities

无。现有主规约目录为空。

## Non-goals

本迭代不做 RAG 查询、Embedding、向量数据库、文本切片、AI 问答、Agent、Assistant、Skill、作业发布、作业提交、作业批改、搜索、用户注册、忘记密码、班级管理后台、用户管理后台、材料编辑、材料删除、OCR、旧版 .doc、.docm、文档密码解密、文档版式还原和浏览器内 PDF/Word 原文档预览、DOCX 页眉页脚/批注/修订历史/文本框/嵌入对象解析。不为这些功能创建表、API 或实现任务。知识库仅存储每份材料的一条提取文本记录，不做切片。进程崩溃或提交结果未知时的自动孤立文件扫描/定时恢复为后续强化项，不作为本迭代交付门槛；明确失败的回滚和文件补偿仍必做。

## Impact

新增前端、Go API、五张业务表及迁移/seed、私有上传卷、Nginx/Compose 配置与测试。固定 API 为 POST /api/login、POST /api/logout、GET /api/me、GET /api/materials、GET /api/materials/{id}、GET /api/materials/{id}/file、POST /api/materials、GET /health。

新增依赖限于当前需求，包括 React 18/Vite/TypeScript、Go MySQL 驱动、bcrypt、API 镜像内的 Poppler 命令工具；DOCX 使用 Go 标准库 ZIP/XML 解析；不引入 Flask、SQLite、Jinja SSR，也不使用可信客户端 signed-cookie 身份。数据库和上传数据使用持久化卷，真实秘密仅从服务端环境变量注入，仓库只提供空值 .env.example。


