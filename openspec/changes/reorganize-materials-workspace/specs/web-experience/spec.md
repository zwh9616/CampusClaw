## ADDED Requirements

### Requirement: WEB-06 Task-focused materials workspace

登录后的材料页面 MUST 提供明确的「材料」「问答」「检索」工作区切换入口，并默认显示「材料」。每次仅将当前工作区的主要内容置于页面阅读流中；用户切换工作区后 MUST 能继续使用本次会话中已输入的查询、问题与已获得的结果。教师的上传入口 MUST 位于材料工作区，索引操作 MUST 与对应材料关联；学生 MUST NOT 看到这些教师操作。工作区切换 MUST 可用键盘完成，并有可辨识的当前状态和可见焦点。

#### Scenario: WE17 Default workspace and role controls

- **GIVEN** 教师或学生已登录
- **WHEN** 打开材料页面
- **THEN** 首先看到材料列表及「材料」当前状态；教师可从材料区进入上传及对应材料的索引操作，学生无这些入口；问答与检索可通过明确的切换入口访问

#### Scenario: WE18 Switch tasks without losing context

- **GIVEN** 用户已在检索或问答工作区提交内容并得到结果
- **WHEN** 切换到「材料」，随后返回原工作区
- **THEN** 之前的输入和结果仍可查看，材料列表不因检索结果的长度而移至页面下方

### Requirement: WEB-07 Progressive display of hits and citations

检索命中和问答出处 MUST 保留服务端给出的顺序、材料标题、切片位置、字符区间及打开材料的入口。长摘录 MUST 先以紧凑预览呈现，并提供展开完整纯文本与收起的操作。问答回答 MUST 保持醒目；点击回答中的有效引用编号 MUST 将对应的同序出处置于可见位置并展开其摘录。无命中、无出处以及请求失败的既有语义 MUST 保留，页面 MUST NOT 展示原始向量分量或网关密钥。

#### Scenario: WE19 Long search results stay navigable

- **GIVEN** 本班检索返回多条长摘录
- **WHEN** 用户浏览结果并展开、收起任一命中
- **THEN** 命中保持原顺序，默认显示紧凑预览，展开后可查看完整安全文本及来源位置；桌面与 390px 宽视口均无横向页面滚动

#### Scenario: WE20 Answer marker reveals matching source

- **GIVEN** 回答包含有效标记 [1] 和 [2]，并有两条同序出处
- **WHEN** 用户点击或用键盘激活 [2]
- **THEN** 第二条出处被定位、突出并展开完整摘录，仍可打开其材料；答案及其他出处保持可访问

#### Scenario: WE21 Empty and failed retrieval remain distinct

- **GIVEN** 用户先得到空检索结果，随后遇到检索服务故障
- **WHEN** 分别查看两次状态
- **THEN** 空结果显示无命中提示，故障显示可重试错误；两者均不生成虚构出处

### Requirement: WEB-08 In-context material reading

从材料列表、检索命中或问答出处打开材料时，详情 MUST 在无需穿过其他工作区长结果的位置展示，并提供关闭或返回入口。关闭详情后 MUST 返回原工作区及其输入、结果和阅读位置。详情 MUST 继续以安全纯文本显示并遵守服务端班级授权；窄屏阅读、键盘焦点及关闭操作 MUST 可用。

#### Scenario: WE22 Open and return from a cited material

- **GIVEN** 用户在问答工作区已展开第二条出处
- **WHEN** 打开该出处的材料详情，再关闭详情
- **THEN** 可阅读授权材料的纯文本内容，关闭后回到原答案与已展开的第二条出处，无需重新提问或滚过其他结果

#### Scenario: WE23 Narrow-screen material reading

- **GIVEN** 用户在 390px 宽视口查看检索命中
- **WHEN** 打开并关闭来源材料，或使用键盘完成相同操作
- **THEN** 详情与关闭入口可读可用、焦点位置合理，返回后检索结果仍在，页面无横向滚动
