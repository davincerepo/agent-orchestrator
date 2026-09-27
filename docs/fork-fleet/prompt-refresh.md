# 会话提示词重载（prompt refresh）

分支：`fleet/feat/prompt-refresh`。基于 `fleet/feat/common`，集成到 `main-fleet`。

## 行为

修改项目角色设定（`agentRules`/`agentRulesFile`/`orchestratorRules`）后，运行中的 Chat 会话通过
`POST /api/v1/sessions/{id}/prompt/reload` 或 `ao session reload-prompt <id>` 立即换用新提示词：
守护进程重算 standing prompt，把 provider 会话复制为**继承全部对话历史**的新会话（新 provider
会话 id），关闭旧宿主进程，冷恢复新会话并绑定回原 AO session。AO session、workspace、git 分支、
时间线全部不变；旧 provider 会话文件保留在磁盘上可回退。对话内容逐字保留，复制的唯一省略项是
**旧提示词在 provider 会话记录里的持久化形式**（保留即新旧提示词共存，等于追加语义）。

- 仅支持 Chat 模式的 codex 与 claude-code；其他 provider 返回 `PROMPT_RELOAD_UNSUPPORTED`。
- 会话有运行中 turn 或排队消息时拒绝（`CHAT_TURN_RUNNING`），空闲后重试。
- 提示词替换后，`startConfigs` 缓存同步更新：后续编辑/分支操作不会把旧提示词带回 provider。

## 机制（实验验证，2026-09-27）

两个 provider 都把提示词持久化进会话记录，因此"原地替换"（resume/fork/settings 覆盖）全部无效：

| | Codex 0.157.0 | Claude Code（claude-agent-acp） |
| --- | --- | --- |
| 提示词持久化形式 | rollout 里的 `role:"developer"` 消息（上下文重建时重复注入） | transcript 每turn的 `attachment{type:"prompt_snapshot"}` |
| 复制方式 | `thread/start {developerInstructions: 新}` + `thread/inject_items`（源 rollout 的 response_items，省略 developer 消息） | transcript 手术复制：逐字保留对话、丢弃 prompt_snapshot、sessionId 改写为新 uuid |
| 新提示词生效点 | thread/start 参数 | `session/load` 的 `_meta.systemPrompt{append}`（系统提示词不在 transcript 内，每次进程启动重建） |

能力接口：`ports.ChatPromptRefresher`（conversation 级，codex）与 `ports.ChatDriverPromptRefresher`
（driver 级，claude；手术复制只触达文件，不需要活连接）。service 编排复用 EditMessage 的分支替换
流程（close 源 controller → 冷 Resume → `CreateAndActivateConversationBranch` → controller 交换），
失败路径经 `restoreClosedSourceController` 恢复原会话。

## 边界

- Codex 侧需读取源线程 rollout（`CODEX_HOME/sessions/**/rollout-*<threadId>.jsonl`，跳过 .zst），
  rollout 未物化时（首个用户消息前）重载失败，不影响原会话。
- Claude 侧复制件写入源 transcript 同目录；复制成功但后续恢复失败时会留下一个孤儿文件（无害，
  与 provider 自身 fork 的行为一致）。
- 宿主进程关闭后冷恢复：与编辑消息的 native fork 同款序列，in-flight 工作由忙时拒绝门禁排除。

## 升级注意

- 依赖 codex `thread/inject_items`（0.157.0 无实验门禁）与 Claude transcript 的 `prompt_snapshot`
  附件类型；上游格式变化时以实验脚本（本分支 PR 描述附结果）重新验证。
- 本分支基于 common，缺 `codex/fix-chat-history-edit` 的 controller owner 同步修复；**连续两次
  重载的回归测试在合入 `main-fleet` 后补充**。
