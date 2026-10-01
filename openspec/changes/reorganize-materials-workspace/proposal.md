## Why

当前材料页把检索、问答、上传、材料列表和详情连续排在同一页面。检索命中与问答出处全部展开时，结果会把材料列表和详情推到很远的位置，师生难以在查找与阅读之间切换。

## What Changes

- 将材料页组织为「材料」「问答」「检索」三个清晰的工作区，默认显示材料；教师上传和索引操作在相关材料上下文中提供。
- 问答先突出答案，出处保留编号与材料信息，长摘录按需展开；点击答案的引用编号时定位并展开对应出处。
- 检索命中先显示紧凑摘要，用户可以展开完整摘录和打开材料；长结果不再决定材料列表在页面中的位置。
- 从问答或检索打开材料时，在当前任务旁展示安全纯文本详情，并提供明确的返回方式；窄屏仍可完整使用。
- 保留已有检索模式、出处顺序、角色权限、错误状态和视觉语言。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `web-experience`: 增加材料工作区导航、渐进展示长检索结果与出处，以及不丢失查找上下文的材料阅读要求。

## Impact

涉及 `frontend/src/pages/MaterialsPage.tsx`、`frontend/src/components/RetrievalPanel.tsx`、`frontend/src/components/IndexPanel.tsx`、`frontend/src/styles.css` 和前端/浏览器验收。沿用 React、现有 `/api` 与服务端授权，不要求后端接口或数据库变更。此方案以 `add-traceable-vector-retrieval` 已提供的检索和问答行为为基础；其增量规格尚未进入主规格，实施前需核对两项变更的规格关系。
