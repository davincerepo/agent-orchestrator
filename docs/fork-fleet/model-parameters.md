# Model parameters

分支：`fleet/feat/model-parameters`。基于 common；Effort 使用官方 `agentConfig.effort`、模型能力目录和选择控件，Fleet 补充 Codex Fast 与参数恢复、反显。

- `serviceTier`：`priority` 开启 Fast，`default` 明确关闭，未设置时继承。只向 Codex 传递；界面仅对能力目录声明支持的模型显示 Fast，未知自定义模型不借用其他模型的能力。
- Worker、Orchestrator、Reviewer 可独立保存默认值；新任务和 Chat 下一轮可覆盖。Reviewer 角色配置优先于公共配置，恢复已有会话不套用后来修改的项目默认值。
- Chat 重连持久 provider 后按需读取已加载线程的真实设置；线程通知更新反显。进入会话重新查询，失败显示未确认，不拿全局默认或旧缓存冒充真实值。
- Terminal 每次进入读取当前运行代际对应的 Codex 原生历史，显示 Model / Effort / Fast。只读已保存记录，未落盘变化下次进入才可见；未知值和读取失败保留为未确认。
- Chat / Terminal 切换在源端停止并落盘后读取参数，与控制权切换一起提交；Chat 明确的下一轮选择优先于原生快照，Terminal 原生状态优先于旧 Chat 设置。读取失败中止切换，保留官方回滚机制。
- 兼容旧配置 `reasoningEffort`，再次保存只写官方 `effort`，显式 `effort` 优先。旧 Fleet 的 0140 参数迁移自动识别并迁至 0148，使官方 0140 standalone 迁移仍能执行，保留参数数据。

同步官方时重点核对：Codex thread/resume 与设置通知语义、能力目录、控制权切换事务、数据库迁移编号。参数读取不代表本轮已采用该设置，Chat 下一轮覆盖与 provider 实际返回值分别保留。
