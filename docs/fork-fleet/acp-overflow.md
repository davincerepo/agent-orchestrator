# ACP notification overflow

本地分支：`codex/fix-acp-overflow`，保留 [官方 PR #6027](https://github.com/Untrivial-ai/agent-orchestrator/pull/6027) 的原始提交，head 为 `3bbcb95eb0f73f38e0511df75a01fa041a2b06c2`。直接合入 `main-fleet`，不重写 PR，也不合入 common 或其他功能分支。

- AO 的 ACP 传输层在 SDK 前限制最多 512 个尚未处理完成的 `session/update`，降低持久 host 重放通知时冲满 SDK 1024 条队列并断开连接的风险。SDK 完成通知回调后释放额度。
- 等待额度超过 2 秒会继续读取，这是 PR 原有的防永久卡死措施；因此它不是任何情况下都不会溢出的绝对保证。
- daemon 启动协调时，只有确认持久 host 仍存活，才重连并修复误记为 Exited 的 Chat 会话。要求实际连接到仍存活的 provider；探测失败、host 已死或被用户主动退出时，不擅自复活会话。这不是运行期间无限自动重试机制。
- 回归覆盖持久 host 大批通知重放、重连后活动状态恢复，以及拒绝用新 provider 冒充存活重连。实际运行版本要在重新构建并启动 Fleet 后才包含本修复。

后续官方合入等价修复后，检查传输背压和恢复回归测试再移除本地差异。保留 Git 合并关系，避免把原 PR 当成新补丁重复重放。
