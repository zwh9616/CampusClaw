## ADDED Requirements

### Requirement: WEB-04 Class-scoped search and source display

登录的教师和学生 MUST 能从材料页面输入自然语言查询并选择 keyword、vector、hybrid，默认 hybrid；页面 MUST 显示检索状态、按次序排列的本班命中、材料标题、切片序号、字符区间、纯文本摘录和打开原材料的入口。无命中 MUST 显示「资料中未找到相关内容」，不能把空结果当作网络错误。401 MUST 清理内存身份并回到登录页；400 MUST 提示修改输入，503 MUST 显示可重试状态。检索页面 MUST 使用同源 /api、沿用 WEB-03 的视觉语言，并在桌面和 390px 窄屏可用、无横向滚动且有可见键盘焦点。MUST NOT 在页面中展示原始向量分量或网关密钥。

#### Scenario: WE10 Search and inspect citation
- **GIVEN** Student A1 已登录且本班有 ready 切片
- **WHEN** 分别选择三种模式查询，并打开一条命中的来源材料
- **THEN** 页面显示对应模式、材料标题、切片序号、字符区间和安全文本摘录，点击来源沿用本班授权详情；桌面与 390px 视口的操作均可用

#### Scenario: WE11 Empty, invalid and unavailable search
- **GIVEN** 用户处于检索页面
- **WHEN** 依次提交无依据问题、空输入，并在向量服务不可用时选择 vector
- **THEN** 依次显示固定无依据提示、输入校验提示和可重试故障提示，不显示虚构出处或原始向量

### Requirement: WEB-05 Evidence-backed answer and teacher index controls

页面 MUST 提供简短问答入口，将回答中的 [1]、[2] 等标记与同序出处列表关联；没有引用时 MUST 显示固定无依据文案和空出处。教师 MUST 能为上传选择切分策略，并对本班材料显式发起重建索引、查看 pending/ready/failed 状态及失败后的重试入口；学生 MUST NOT 看到重建控制，但仍可检索本班 ready 内容。前端隐藏控制 MUST NOT 代替服务端授权。回答、出处及索引状态在桌面和 390px 视口 MUST 清晰可用。

#### Scenario: WE12 Ask with aligned citations
- **GIVEN** 本班有支持答案的两条切片
- **WHEN** 学生提问并选择回答中的 [2]
- **THEN** 页面中 [2] 对应第二条出处，可查看材料标题、切片位置和授权材料详情；无依据时答案为固定文案且没有出处

#### Scenario: WE13 Teacher rebuild and student restriction
- **GIVEN** 教师与学生分别登录同一班级
- **WHEN** 教师选择 hierarchy 重建本班材料，学生随后浏览材料与检索页面
- **THEN** 教师可见索引进度及失败重试；学生无重建入口，只看可用的 ready 命中；伪造页面角色不改变服务端权限
