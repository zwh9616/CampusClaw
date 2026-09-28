# compose-runtime Specification

## Purpose
TBD - created by archiving change add-auth-class-materials. Update Purpose after archive.

## Requirements

### Requirement: RUN-01 Fixed stack and one-command startup

系统 MUST 使用 React 18 + TypeScript + Vite、Go（net/http）、MySQL 8、Nginx、Docker Compose。Compose MUST 至少含 web=Nginx 静态前端/API 代理、api=Go、db=MySQL 8，MUST NOT 使用 Flask、SQLite、Jinja SSR 或一个容器承载前端/API/数据库。配置服务端秘密后 MUST 可通过 docker compose up --build 自动构建、迁移、seed 并启动，不要求手工 SQL/前端编译。数据与文件 MUST 使用持久化卷。API 镜像 MUST 包含文档解析所需命令，缺失时 MUST 启动失败并给出不含秘密的提示，MUST NOT 把故障伪装成用户文件损坏。

#### Scenario: AC22 One-command startup
- **GIVEN** Docker Compose 可用，已按 .env.example 配好未跟踪的环境秘密，干净测试卷
- **WHEN** 在项目根执行 docker compose up --build
- **THEN** web/api/db 就绪，http://localhost:8080 可打开 React 登录页，可用种子账号登录，无额外手工初始化步骤

### Requirement: RUN-02 Nginx-only browser entry

正常运行 MUST 仅 web 发布宿主机 8080:80，api/db MUST NOT 发布宿主机端口。浏览器链路 MUST 为 Browser → Nginx → React 静态文件或 Go API → MySQL；浏览器 MUST NOT 直接访问 API 容器或数据库。Nginx / MUST 提供 SPA，/api/* 和精确 /health MUST 反向代理 Go，MUST NOT 将未知 API 错误回退成 index.html。

#### Scenario: AC23 Browser uses only port 8080
- **GIVEN** 正常 Compose 启动
- **WHEN** 浏览器完成登录、列表、详情、上传、下载、登出，并检查网络记录及 Compose 端口配置
- **THEN** 所有业务请求仅访问 localhost:8080，只有 web 存在宿主机端口映射，SPA 路由刷新可用

#### Scenario: AC24 MySQL has no host port
- **GIVEN** db 没有 ports，api 也没有宿主机 ports
- **WHEN** 通过 Nginx 登录、上传和下载
- **THEN** 全部正常，api 使用内部 db:3306，docker compose ps/config 中无数据库宿主机端口映射

### Requirement: RUN-03 Private uploads

Nginx MUST 对 /uploads 及 /uploads/* 返回 404，MUST NOT 静态暴露、代理公开或 SPA 回退这些路径。web MUST NOT 挂载上传卷。原文件 MUST 只能经 Go 认证与班级授权后的下载 API 获取。

#### Scenario: AC14 Direct upload paths inaccessible
- **GIVEN** 有已上传文件且测试知道实际 class_id/stored_filename
- **WHEN** 分别以未登录、Teacher A、Student B1 请求 /uploads/{class_id}/{stored_filename}，并请求 /uploads
- **THEN** 均 404，不返回原始文件字节，只有本班授权下载 API 能取得文件

### Requirement: RUN-04 Public health endpoint

GET /health MUST 无需登录、经 Nginx 转发至 Go，在 API 和数据库就绪时返回 200 {status:"ok"}，依赖不可用返回 503 通用错误，MUST NOT 泄露秘密。

#### Scenario: AC21 Health through Nginx
- **GIVEN** Compose 所有依赖就绪，无 Session Cookie
- **WHEN** GET http://localhost:8080/health
- **THEN** 返回 200 及 JSON status=ok

#### Scenario: RU01 Database unavailable
- **GIVEN** 已启动系统但测试暂停数据库服务
- **WHEN** GET /health
- **THEN** 在超时上限内返回 503 通用响应，数据库恢复后返回 200，不输出 DSN/密码

#### Scenario: RU02 Document parser is packaged
- **GIVEN** 使用项目 Dockerfile 构建的 API 镜像
- **WHEN** 启动系统并通过 Nginx 上传合法 PDF/DOCX，再以测试镜像移除必需 PDF 解析命令启动
- **THEN** 正常镜像可解析两种格式，无须宿主机安装工具；缺失依赖的测试镜像启动失败，日志不含秘密

### Requirement: RUN-05 Local Vite development API proxy

在正常 Compose 的 Nginx 入口 http://localhost:8080 已就绪时，本机 Vite 开发服务器 MUST 固定监听 http://localhost:5173，端口被占用时 MUST 明确失败而不自动切换端口。Vite MUST 将 /api/* 请求原路径代理到 http://localhost:8080，由现有 Nginx 再转发给 Go；浏览器 MUST 继续使用相对 /api 地址与同源 Cookie，MUST NOT 请求 API 容器地址或依赖 CORS。仅在本地开发时，Compose MAY 将显式配置的 DEV_PUBLIC_ORIGIN=http://localhost:5173 传给 API，且 MUST 满足 AUTH-05 的启用限制。正常 Compose 浏览器入口和 api/db 不发布宿主机端口的要求 MUST 保持有效；Vite 开发服务器仅监听本机。

#### Scenario: AC32 Vite dev uses same-origin API paths
- **GIVEN** Compose 在 http://localhost:8080 正常运行，PUBLIC_ORIGIN=http://localhost:8080、SESSION_COOKIE_SECURE=false、DEV_PUBLIC_ORIGIN=http://localhost:5173，且本机执行 npm run dev
- **WHEN** 浏览器打开 http://localhost:5173，依次登录、查询 /api/me 和材料列表、由教师上传材料并登出，再尝试从外站 Origin 发送登录 POST
- **THEN** 浏览器业务请求始终以 http://localhost:5173/api/* 发出，代理保持路径与 Cookie，登录/上传等同源 POST 成功；外站 Origin 仍为 403，8080 正常入口仍可使用，api/db 无宿主机端口映射
