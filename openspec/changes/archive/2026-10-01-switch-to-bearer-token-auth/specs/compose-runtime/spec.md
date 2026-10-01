## MODIFIED Requirements

### Requirement: RUN-02 Nginx-only browser entry

正常运行 MUST 仅 web 发布宿主机 8080:80，api/db MUST NOT 发布宿主机端口。浏览器链路 MUST 为 Browser → Nginx → React 静态文件或 Go API → MySQL；浏览器 MUST NOT 直接访问 API 容器或数据库。Nginx / MUST 提供 SPA，/api/* 和精确 /health MUST 反向代理 Go，并 MUST 将 Authorization 请求头原样传至 Go、MUST NOT 剥离或改写它，MUST NOT 将未知 API 错误回退成 index.html，也 MUST NOT 将 Bearer token 写入访问日志。鉴权链路 MUST NOT 依赖任何 Cookie：代理 MUST NOT 为鉴权保留或注入 Cookie。

#### Scenario: AC23 Browser uses only port 8080
- **GIVEN** 正常 Compose 启动
- **WHEN** 浏览器以 Bearer token 完成登录、列表、详情、上传、下载、登出，并检查网络记录及 Compose 端口配置
- **THEN** 所有业务请求仅访问 localhost:8080，Authorization 头到达 Go，只有 web 存在宿主机端口映射，SPA 路由刷新可用，且整个流程中没有任何 Cookie 被设置或发送

#### Scenario: AC24 MySQL has no host port
- **GIVEN** db 没有 ports，api 也没有宿主机 ports
- **WHEN** 通过 Nginx 登录、上传和下载
- **THEN** 全部正常，api 使用内部 db:3306，docker compose ps/config 中无数据库宿主机端口映射

### Requirement: RUN-05 Local Vite development API proxy

在正常 Compose 的 Nginx 入口 http://localhost:8080 已就绪时，本机 Vite 开发服务器 MUST 固定监听 http://localhost:5173，端口被占用时 MUST 明确失败而不自动切换端口。Vite MUST 将 /api/* 请求原路径与 Authorization 请求头代理到 http://localhost:8080，由现有 Nginx 再转发给 Go；浏览器 MUST 继续使用相对 /api 地址和 Bearer 请求头，MUST NOT 请求 API 容器地址或依赖 CORS，也 MUST NOT 依赖任何 Cookie。仅在本地开发且 PUBLIC_ORIGIN=http://localhost:8080 时，Compose MAY 将显式配置的 DEV_PUBLIC_ORIGIN=http://localhost:5173 传给 API；非此组合 MUST 拒绝启用开发来源。正常 Compose 浏览器入口和 api/db 不发布宿主机端口的要求 MUST 保持有效；Vite 开发服务器仅监听本机。

#### Scenario: AC32 Vite dev uses same-origin API paths
- **GIVEN** Compose 在 http://localhost:8080 正常运行，PUBLIC_ORIGIN=http://localhost:8080、DEV_PUBLIC_ORIGIN=http://localhost:5173，且本机执行 npm run dev
- **WHEN** 浏览器打开 http://localhost:5173，依次登录、查询 /api/me 和材料列表、由教师上传材料并登出，再尝试从外站 Origin 发送登录 POST
- **THEN** 浏览器业务请求始终以 http://localhost:5173/api/* 发出，代理保持路径与 Authorization 头，登录/上传等同源 POST 成功；外站 Origin 仍为 403，8080 正常入口仍可使用，api/db 无宿主机端口映射
