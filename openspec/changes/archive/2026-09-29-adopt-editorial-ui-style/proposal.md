## Why

现有前端只有少量零散样式变量，缺少可延续的视觉方向。用户已选择「书卷文雅」方案，需要让登录页和材料页采用统一风格，并给后续页面留下明确的设计约束。

## What Changes

- 登录页和材料页采用米白纸张感、北大红重点操作、克制的衬线标题与清晰的内容层级。
- 将常用颜色、间距、圆角和阴影集中为设计变量，复用品牌、按钮、表单和卡片样式，并适配窄屏。
- 增加前端视觉规范和开发指引，让后续 UI 改动沿用该方向；将规范位置写入 OpenSpec 项目背景。
- 保留现有登录、材料、上传、权限与错误处理行为及浏览器验收脚本使用的页面入口。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `web-experience`：增加登录页和材料页的统一视觉、响应式布局及后续页面复用要求。

## Impact

涉及 `frontend/src/pages/`、`frontend/src/styles.css`、共享品牌组件、`frontend/STYLE_GUIDE.md`、`frontend/AGENTS.md` 和 `openspec/config.yaml`。不涉及 API、数据库或新增运行时依赖。

## Process note

本变更是对已在工作树实施的 UI 改动进行补录和核验；OpenSpec 变更创建时间晚于代码修改，不能视为事前审批。
