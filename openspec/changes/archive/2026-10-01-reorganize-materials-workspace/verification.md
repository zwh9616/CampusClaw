# UI 改版验收记录

- 前端：`npm run typecheck`、`npm test`（26 项）、`npm run build` 均通过。
- Playwright CLI：在运行中的 Vite 入口（5173）以教师和学生账号检查 1440×900 与 390×844 视口。教师材料列表、上传入口、材料阅读、单条索引；学生材料列表、无教师入口、标签键盘方向键切换均通过。两种视口的 `document.documentElement.scrollWidth` 均等于视口宽度。
- 检索：真实 API 查询“向量检索”返回按序命中，结果独立于材料列表，材料入口可打开阅读层。问答真实 API 本次返回“资料中未找到相关内容”；为验证有引用时的交互，在同一浏览器中临时覆盖 `/api/ask` 响应，确认长出处默认收起，点击 `[2]` 后第二条获得焦点、滚动到可见区域并展开全文。覆盖已在切换学生账号时移除。
- 局部截图保存在本地 `output/playwright/`：`materials-desktop.png`、`materials-mobile.png`、`reader-mobile.png`、`search-mobile.png`、`citation-mobile.png`、`student-desktop.png`。浏览器临时快照和截图不入库。

- 实际访问入口：2026-10-01 重新构建并替换 Docker 的 `web` 容器后，`http://localhost:8080` 提供新版资源 `index-ikD9bjBO.js` 和 `index-CmnYgy2A.css`。Playwright 在此入口登录教师账号，确认默认选中“材料”及切换“问答”可用；截图为本地 `output/playwright/live-8080.png`。
