## ADDED Requirements

### Requirement: WEB-03 Consistent editorial interface

登录页和材料页 MUST 采用统一的「书卷文雅」视觉语言：以米白背景和纸张色内容面为基础，以北大红 `#94070A` 表达品牌和主要操作，以清晰易读的正文及克制的衬线标题组织信息。后续新增的前端页面 MUST 延续同一视觉语言；错误和成功状态 MUST 与品牌强调色区分。界面 MUST 在桌面和 390px 宽的窄屏视口下保持内容与操作可读、可用，且不得产生横向页面滚动。键盘操作时 MUST 有可见焦点。

#### Scenario: WE07 Login appearance and narrow layout

- **GIVEN** 访客尚未登录
- **WHEN** 分别在桌面和 390px 宽视口打开登录页
- **THEN** 显示统一品牌、账号表单与北大红主要按钮；窄屏下内容按单栏排列，输入和按钮可用，页面不产生横向滚动

#### Scenario: WE08 Materials appearance for both roles

- **GIVEN** 教师或学生已登录
- **WHEN** 分别在桌面和 390px 宽视口打开材料页
- **THEN** 用户信息、材料列表与可用操作沿用登录页的视觉语言；教师的上传区在窄屏下与列表按单栏排列，学生仍无上传入口，页面不产生横向滚动

#### Scenario: WE09 Focus and status distinction

- **GIVEN** 用户使用键盘操作登录页或材料页
- **WHEN** 焦点进入可交互控件，或页面显示错误、成功状态
- **THEN** 焦点清楚可见；错误与成功状态可辨识，且不依赖北大红品牌强调色表达状态
