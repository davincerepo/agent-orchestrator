# Model parameters

分支：`fleet/feat/model-parameters`。Effort 配置、Session 快照、存储和选择控件使用官方实现；Fleet 只补充 Codex Fast 与原生参数恢复、反显。本分支因依赖官方新增的 Session `Effort` 字段而更新 common，普通官方同步不再要求各功能分支跟进。

- `serviceTier`：`priority` 开启 Fast，`default` 明确关闭，未设置时继承。只向 Codex 传递；界面仅对能力目录声明支持的模型显示 Fast，未知自定义模型不借用其他模型的能力。
- Worker、Orchestrator、Reviewer 可独立保存默认值；新任务和 Chat 下一轮可覆盖。Reviewer 角色配置优先于公共配置，恢复已有会话不套用后来修改的项目默认值。
- Chat 重连持久 provider 后按需读取已加载线程的真实设置；线程通知更新反显。进入会话重新查询，失败显示未确认，不拿全局默认或旧缓存冒充真实值。
- Terminal 每次进入读取当前运行代际对应的 Codex 原生历史，显示 Model / Effort / Fast。只读已保存记录，未落盘变化下次进入才可见；未知值和读取失败保留为未确认。
- Chat / Terminal 切换在源端停止并落盘后读取参数，与控制权切换一起提交；Chat 明确的下一轮选择优先于原生快照，Terminal 原生状态优先于旧 Chat 设置。读取失败中止切换，保留官方回滚机制。
- Session 只读写官方 `Effort/effort`。升级时一次性搬迁旧 `sessions.reasoning_effort` 数据并删除旧列；已有非空官方值优先。官方 Chat 设置中的 `ReasoningEffort` 与 provider 协议字段保持原样。
- Claude 网关目录区分"未申报"与"不支持"：`/v1/models` 不带 `capabilities` 的网关模型标记 `effortUnknown`，配置的 effort 原样透传，由 Claude Code（真正的模型元数据所有者，ACP 会话内校验具体档位）接受或拒绝；只有 provider 明确申报档位列表（或明确 `supported:false`）时 AO 才做白名单校验和拒绝。第一方 Anthropic API 的空列表仍是"该模型不支持 effort"的真实答案。
- Fleet 的 Fast 列通过独立、幂等的 schema 扩展管理，不再占用官方 goose 迁移编号。旧 Fleet 使用过的 0140/0148 仅在确认官方对应 schema 未执行时释放，保留已有 Fast 数据。

## 回显：官方基线与 Fleet 差异

官方按"下一轮参数由谁执行"分层回显，Fleet 保留该分层，只收紧 Codex 侧显示值的来源：

- 官方基线：ACP 系 harness（如 Claude Code）输入框显示 provider 会话的实时 config options，`default` 占位按官方实现改名显示；Codex 输入框显示 AO 自存的下一轮设置，未显式选择时回落线程默认值，该默认来自 `thread/start` / `thread/resume` 响应记录的线程状态；Start/Resume 均把自存 model/effort 作为 override 下发，意图与实测是两个分别保留的事实。
- 官方缺口：复用持久 host 的 live-rejoin 把 AO 意图直接当作线程状态写入显示缓存；不消费 `thread/settings/updated` 通知；重进会话不重读线程真实设置；没有 Terminal 显示与 Chat/Terminal 切换场景。
- Fleet 差异：live-rejoin 不预填意图；进入会话经 loaded-thread 成员检查后以 bare `thread/resume` 读取真实设置；设置通知即时更新缓存且视为更新证据，每轮 `turn/start` 后缓存失效、下次进入重读；未知或读取失败显示未确认，不用意图、全局默认或旧缓存冒充实测。Terminal 显示与切换规则见上。官方同步时若改动了 ACP config options 或 Codex 线程状态语义，需对照本节核对差异。
- 已知边界：输入框的模型/参数控件同时承担"用户选择"与"回显生效值"两个职责，且回显语义随 harness 而不同——ACP 系显示 provider 已确认的生效值，写入为非乐观更新（请求期间控件禁用，回包原子替换目录，失败保持旧值并显示错误，均为官方实现）；Codex 显示 AO 已保存的下一轮意图，保存成功即回显、下一轮下发。两层语义均正确且不存在假回显，但界面未标注当前显示的到底是哪一层，是排障时"显示值含义不清"的根源。后续若改进，方向是显式标注回显来源并提供 provider 实测值的对比视图，而不是改写更新时序。

验收以相对官方的代码差异为准：不再引入第二套 Session Effort 字段或业务读写链，不整笔重放旧模型参数补丁。同步时核对 Codex thread/resume 与设置通知、能力目录和控制权切换事务。参数读取不代表本轮已采用该设置，Chat 下一轮覆盖与 provider 实际返回值分别保留。
