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

统一流程（service 层编排，两个 provider 一致）：

```
① 忙时门禁：turn 运行中/有排队 → 拒绝（CHAT_TURN_RUNNING）
② session_manager.buildSystemPrompt 从当前项目 rules 重算提示词
③ 调 provider 能力接口做"会话复制" → 新 provider 会话 id        ← 两 provider 唯一差异点
④ 关闭旧宿主进程（旧会话停住，不再被持有）
⑤ 冷 Resume/Load 新 id，携带新提示词                             ← 新提示词生效点
⑥ 分支事务重绑（新 id 写回 session）+ controller 交换
   失败 → 恢复原会话；成功 → startConfigs 缓存换新提示词
```

**为什么"杀进程 → 起新进程 → 继续原会话"不能刷新提示词**：提示词不在进程里，而在会话数据里。
新进程 resume 原会话 id 时会重放同一份记录 —— codex 的 developer 消息、claude 的
prompt_snapshot 附件随之回到模型上下文，旧提示词继续生效（两者均实验证实：冷重启 +
resume 带新提示词参数，模型仍按旧提示词回答）。必须先有一份**剥离了提示词记录的会话副本**，
新进程加载副本时才没有旧提示词可回放。

以 Claude 为例（Codex 同构），生效点前后的进程与数据状态：

| 步骤 | 进程 | 会话数据 | 提示词状态 |
| --- | --- | --- | --- |
| ③ 复制 | 旧进程仍活着 | 新文件：对话逐字复制，prompt_snapshot 已剥离 | 尚未生效 |
| ④ 关旧宿主 | 旧适配器 + claude 进程退出 | 原文件保留（可回退），副本就绪 | — |
| ⑤ 冷 Load | **全新 claude 进程**启动 | 加载剥离后的副本 | 新 append 以进程启动参数注入，上下文里已无旧快照 → 替换完成 |

Codex 时序略不同：③ 的 `thread/start` 带 `developerInstructions` 时新提示词已烘焙进新线程
（codex 为它写新的 developer 消息），⑤ 的 resume 只是新宿主挂载新线程，提示词参数再带一次
属双保险。

步骤 ④ 关旧宿主不是为了刷新提示词，而是**释放会话持有者**：活着的 codex 线程处于 running
状态会直接忽略 instructions 覆盖（"override was provided and ignored while running"）；ACP
活会话不会重发 `session/load`，新会话 id 必须由不带旧会话的新宿主加载。

一句话：**换进程是必要条件**（新提示词参数获得注入机会），**换数据（剥离旧提示词的会话副本）
才是充分条件**；两者合起来是"替换"，只有前者就是"重启也不生效"的现象。

两个 provider 都把提示词持久化进会话记录，因此"原地替换"（resume/fork/settings 覆盖）全部无效：

| | Codex 0.157.0 | Claude Code（claude-agent-acp） |
| --- | --- | --- |
| 提示词持久化形式 | rollout 里的 `role:"developer"` 消息（上下文重建时重复注入） | transcript 每turn的 `attachment{type:"prompt_snapshot"}` |
| 复制方式 | `thread/start {developerInstructions: 新}` + `thread/inject_items`（源 rollout 的 response_items，省略 developer 消息） | transcript 手术复制：逐字保留对话、丢弃 prompt_snapshot、sessionId 改写为新 uuid |
| 新提示词生效点 | thread/start 参数（新线程自己的配置；⑤ 的 resume 再带一次属保险） | `session/load` 的 `_meta.systemPrompt{append}`（新 claude 进程以新 append 启动，加载已剥离快照的副本） |

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
- **集成 checklist**（与 `fleet/feat/model-parameters` 共存时，作为 merge 冲突解决的语义部分补上，
  common 尚无这些字段故分支代码不可直接引用）：
  - codexappserver `RefreshStandingPrompt`：快照读取 `threadServiceTier` 并在替换线程的
    `thread/start` 参数带 `serviceTier`；
  - service/chat `ReloadStandingPrompt` 的 Resume 配置带 `cfg.ServiceTier`。
  待这些字段随上游进入 common 后，本分支 rebase 即可直接引用，此条撤下。
- 本分支基于 common，不含 `codex/fix-chat-history-edit` 临时补丁（按约定仅集成于 `main-fleet`，
  上游修复后撤下）；**连续两次重载的回归测试随 `main-fleet` 集成补充**。
