# Agent 命令

分支：`fleet/feat/agent-commands`。消息 steering 使用官方 `ao send --steer`，包括空闲时新建轮次、投递 ID 与结果恢复。

## 项目范围

AO 会话内以 `AO_SESSION_ID` 对应的数据库项目归属为准，覆盖冲突的 `AO_PROJECT_ID`，Chat/TUI 一致。

- `ao session ls` 默认只查自己的项目；`--project`、`--all-projects` 可显式扩大只读范围，`--all` 仍仅表示包含 orchestrator。
- `send`（含 `--steer`）、`spawn`、delegate 拒绝跨项目派单；scope 校验在 CLI 和 daemon 执行。失效、已终止或无项目的调用者上下文返回错误，不退回全局操作。
- 普通终端和桌面 UI 保留全局管理能力。`callerSessionId` 是防误派上下文，不是鉴权；消息中的 `[from ...]` 不作为身份。
- 已存活 agent 不会自动更新 CLI/PATH；必要时在空闲后 Exit agent → Resume agent。可用 `ao session ls --help` 中的 `--all-projects` 确认 CLI 已更新。

## 消息长度

普通 send 与官方 steer 接口均限制为 1 MiB（1048576 UTF-8 字节，包含发送者前缀），超限在投递前返回 `MESSAGE_TOO_LONG`。更长内容写入接收方可访问的文件，只发送路径；这不改变 provider 自身的上下文限制。
