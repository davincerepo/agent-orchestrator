# 历史编辑与 Agent 切换

分支：`codex/fix-edit-provider-switch`。基于 `fleet/feat/common`，修复上游历史编辑遗漏待执行 Agent 切换的问题，集成到 `main-fleet`。

- 历史编辑携带所选 Agent、模型和 effort，并随投递草稿保存，重试沿用原选择。
- 跨 Agent 编辑复用现有 provider 切换及 `CommitProviderSwitch`，将交接上下文和分支截断点限定在被编辑消息之前。同 Agent 编辑沿用原有流程。
- 接受回执和恢复通过已有消息、turn 查询及 `CompleteEditDelivery` 完成。临时修复集中在发送和服务编排层。

上游提供等价修复后，验证所选参数、上下文截断及重试行为，再移除本分支补丁。
