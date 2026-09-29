# 书卷文雅 UI 验收记录（2026-09-29）

## 范围与命令

- `cd frontend && npm run build`：通过（TypeScript 与 Vite 构建）。
- `node tests/browser/ui-style.mjs`：使用构建产物和本地示例 API 响应，Chromium 检查 1280px 与 390px 的登录、教师、学生页面；两种宽度下各四组检查均通过。
- `git diff --check`：通过。
- `openspec validate adopt-editorial-ui-style --strict`：通过。

## 浏览器结果

| 场景 | 1280px | 390px |
| --- | --- | --- |
| 登录页品牌、表单布局、无横向溢出、可见焦点 | PASS | PASS |
| 教师材料列表与上传区、无横向溢出、可见焦点 | PASS | PASS |
| 学生材料列表、无上传入口、无横向溢出 | PASS | PASS |
| 错误/成功状态色区分；材料脚本内容只按文本显示 | PASS | PASS |

截图：[登录桌面](../../design-preview/beida-red/implemented-login.png)、[登录窄屏](../../design-preview/beida-red/implemented-login-mobile.png)、[教师桌面](../../design-preview/beida-red/implemented-materials.png)、[教师窄屏](../../design-preview/beida-red/implemented-materials-mobile.png)、[学生桌面](../../design-preview/beida-red/implemented-student.png)、[学生窄屏](../../design-preview/beida-red/implemented-student-mobile.png)。

## 真实 API 回归

- 使用 `docker compose build web` 和 `docker compose up -d --no-deps web` 更新 web 容器；API 与数据库保持运行，`http://localhost:8080/health` 返回 200。
- 使用本机 Edge 执行 `node tests/browser/check.mjs`（设置 `CHROME_PATH`，密码从本地 `.env` 的种子账号变量映射）：7 checks，0 failed，退出码 0。
- WE01–WE06、AC23 均为 PASS：覆盖登录与错误凭证、会话恢复、教师上传与查看下载、学生权限和纯文本显示、登出、限流提示，以及浏览器请求仅经 `localhost:8080`。
- 本机 Playwright 缓存中的 Chromium 在等待登录节流时异常退出；切换到本机 Edge 后，原验收脚本和断言完整通过。
