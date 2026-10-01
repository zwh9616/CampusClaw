## ADDED Requirements

### Requirement: RUN-06 Private vector service and model configuration

Docker Compose MUST 增加可由 API 容器访问的 Qdrant 服务及持久化向量数据卷，并保持仅 web 发布宿主机 8080:80；Qdrant、API 和数据库 MUST NOT 向宿主机发布业务端口。配置好服务端嵌入与对话网关地址、模型和密钥后，docker compose up --build MUST 完成新增迁移、向量集合准备和服务启动，无手工建集合步骤；嵌入维度 MUST 与集合配置一致，度量为余弦。网关密钥 MUST 只存在于服务端配置，MUST NOT 出现在前端构建产物、浏览器响应或日志。Qdrant 或网关故障 MUST 通过 KR-07 的检索错误表达，现有材料查看和下载仍可用。

#### Scenario: RU03 One-command startup with private Qdrant
- **GIVEN** 干净持久化卷、有效服务端网关配置和可用 Compose
- **WHEN** 执行 docker compose up --build 并检查端口、卷及浏览器网络记录
- **THEN** Qdrant 集合就绪且数据持久化；浏览器仅经 web 的 /api 使用检索，api/db/Qdrant 无宿主机业务端口，密钥不进入浏览器

#### Scenario: RU04 Vector dependency outage
- **GIVEN** 已有可用材料与 ready 切片
- **WHEN** 暂停 Qdrant 并分别请求材料详情、下载和三种检索
- **THEN** 详情、下载与 keyword 检索保持可用；vector/hybrid 按 KR-07 返回通用 503，不泄露连接信息
