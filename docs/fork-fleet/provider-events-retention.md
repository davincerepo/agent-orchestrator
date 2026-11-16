# Provider 事件归档保留策略

分支：`fleet/feat/provider-events-retention`。基于 `fleet/feat/common`，集成到 `main-fleet`。

## 行为

为 `provider_events` 原始事件归档增加后台清理任务：启动时立即执行一次，之后每小时执行。任务优先按批次删除 48 小时以前的流式 delta，再将全表限制在 200,000 行以内。每批最多删除 10,000 行，每轮最多执行 8 批，避免维护任务长时间占用数据库。

delta 在 projector 处理时已折叠进持久化会话数据；原始行仅用于短期排障，因此过期清理不会删除会话、消息或已经投影的文本。非 delta 事件保留更久，只有超过全表上限时才按最旧优先删除。

## 边界

- 只清理 `provider_events` 归档，不修改 `projects`、`sessions`、`conversations`、`conversation_messages` 或模型用量表。
- 清理条件和总量裁剪都在事务安全的存储查询中按批执行。
- 清理失败只记录警告，不阻止 daemon 启动；下一个周期会重试。
- daemon 关闭时等待清理 goroutine 退出，再关闭存储。

## 升级注意

- 上游若为原始 provider 事件增加运行时重放读者，需要重新确认 48 小时 delta 窗口是否足够。
- 上游若引入同类归档保留策略，应移除本分支的重复 janitor，仅保留仍缺失的策略差异。
- SQL 查询由 SQLC 生成；修改 `queries/conversations.sql` 后运行仓库的 SQLC 生成命令并审查生成差异。
