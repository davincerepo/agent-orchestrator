# AO Fleet fork

原仓库：https://github.com/Untrivial-ai/agent-orchestrator

Fork：https://github.com/davincerepo/agent-orchestrator

集成分支：`main-fleet`。公共基底：`fleet/feat/common`，仅放各功能共用的代码、配置和维护说明，不放某个功能的业务实现。目前没有需要抽取的公共运行时代码。

## 功能分支

| 分支 | 范围 | 说明文件（合入后提供） |
| --- | --- | --- |
| `fleet/fix/windows-codex-accounts` | Windows Codex 账号、ACL 与恢复 | `fix-windows-codex-accounts.md` |
| `fleet/feat/portable` | 免安装、数据与进程隔离、AO Fleet 显示名称 | `fleet-portable.md` |
| `fleet/feat/model-parameters` | Effort/Fast、真实线程参数、Reviewer、Chat/Terminal 同步与显示 | `add-model-params.md` |
| `fleet/feat/agent-commands` | 项目查询与派单范围、消息投递策略与长度限制 | `agent-project-scope.md`、`message-steering.md` |
| `fleet/feat/session-import` | 一次性导入与回滚工具，仅供参考，不合入主分支 | `migrate-official-sessions.md`（仅参考分支） |

## 分支维护约定

- 所有功能与修复都必须先切换到对应的 `fleet/feat/*` 或 `fleet/fix/*` 分支完成并提交，再合并到 `main-fleet`，禁止在 `main-fleet` 直接提交实现。

- 如果使用独立的git worktree开发，用完了记得释放。 

- 分支之间需要尽可能的相互独立，如果合并到主干时发生比较多的冲突，那么分支架构需要重构了，有这种情况及时反馈给我

## 约束

- 需要你补充文档的时候，要简短描述，只写我让你添加的内容或者必要的内容。不用展开描述一堆命令行的用法、以及一些基础常识的东西。
