# Model parameters

分支：`fleet/feat/model-parameters`。Effort 配置、Session 快照、存储和选择控件使用官方实现；Fleet 只补充 Codex Fast 与原生参数恢复、反显。本分支因依赖官方新增的 Session `Effort` 字段而更新 common，普通官方同步不再要求各功能分支跟进。

- `serviceTier`：`priority` 开启 Fast，`default` 明确关闭，未设置时继承。只向 Codex 传递；界面仅对能力目录声明支持的模型显示 Fast，未知自定义模型不借用其他模型的能力。
- Worker、Orchestrator、Reviewer 可独立保存默认值；新任务和 Chat 下一轮可覆盖。Reviewer 角色配置优先于公共配置，恢复已有会话不套用后来修改的项目默认值。
- Chat 重连持久 provider 后按需读取已加载线程的真实设置；线程通知更新反显。进入会话重新查询，失败显示未确认，不拿全局默认或旧缓存冒充真实值。
- Terminal 每次进入读取当前运行代际对应的 Codex 原生历史，显示 Model / Effort / Fast。只读已保存记录，未落盘变化下次进入才可见；未知值和读取失败保留为未确认。
- Chat / Terminal 切换在源端停止并落盘后读取参数，与控制权切换一起提交；Chat 明确的下一轮选择优先于原生快照，Terminal 原生状态优先于旧 Chat 设置。读取失败中止切换，保留官方回滚机制。
- Session 只读写官方 `Effort/effort`。升级时一次性搬迁旧 `sessions.reasoning_effort` 数据并删除旧列；已有非空官方值优先。官方 Chat 设置中的 `ReasoningEffort` 与 provider 协议字段保持原样。
- Fleet 的 Fast 列通过独立、幂等的 schema 扩展管理，不再占用官方 goose 迁移编号。旧 Fleet 使用过的 0140/0148 仅在确认官方对应 schema 未执行时释放，保留已有 Fast 数据。

验收以相对官方的代码差异为准：不再引入第二套 Session Effort 字段或业务读写链，不整笔重放旧模型参数补丁。同步时核对 Codex thread/resume 与设置通知、能力目录和控制权切换事务。参数读取不代表本轮已采用该设置，Chat 下一轮覆盖与 provider 实际返回值分别保留。
