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
| `fleet/feat/project-scope` | 当前项目查询与派单范围 | `agent-project-scope.md` |
| `fleet/feat/session-import` | 一次性导入与回滚工具，仅供参考，不合入主分支 | `migrate-official-sessions.md`（仅参考分支） |

## 分支维护约定

功能分支从公共基底分叉，不合入无关功能；同一功能内保留独立的实现、修复和 CI 提交。每项功能保留实现文档与行为测试。`main-fleet` 通过显式 merge commit 集成已验证分支，用于运行和打包。

同步官方时先更新公共基底，再逐个更新功能分支并在临时集成分支验证。不要将整个 `main-fleet` 合回单个功能分支。共享 API/SQL 生成文件根据各分支实际源代码重新生成，集成时再校验。不修改已经应用的数据库迁移，不把同步上游与不相关功能变更混在一个提交。

官方会话导入属于历史一次性工具，主分支不包含其脚本、命令或运行入口。参考代码可通过 `git show fleet/feat/session-import:frontend/scripts/migrate-fleet.mjs` 查看；该工具的兼容范围固定在对应历史版本，不能作为未来版本的通用迁移工具。

提交和分支均在本地维护，未经用户要求不推送。

## 2026-09-19 历史重组

重组基线为 `cadde8c9f`，原集成分支为 `019a4ad86`。本次不同时更新官方版本。原本地 Fleet 分支通过 `archive/fleet-20260919/<原分支名>` 标签归档。

| 原提交 | 新归属 |
| --- | --- |
| `5780bdcd7` | common 的 Fork 说明；过时的 Guardian 路径不重放 |
| `4526af5c6`、`c072c3db4` | windows-codex-accounts，保留上游修复与补强的独立提交 |
| `e24f8d0ab` | CI 分支触发归 common，账号测试归账号分支的独立 CI 提交 |
| `a9c30cc55` | 模型功能文档，采用已实现版本，避免重放过时设计 |
| `690e76d0e` | portable；公共命名/索引归 common，各功能文档归各自分支 |
| `268ee0a2b` | session-import，仅参考，不集成 |
| `2c6bf9322`、`2f9f5aa5e`、`ba501cdb3`、`019a4ad86` | model-parameters，保留四项独立提交 |
| `1474aebcb` | project-scope |

旧 `05ea11d05` 与 `690e76d0e` 文件树相同；旧 `c424edf3b` 与 `2c6bf9322` 文件树相同，不重复合入。功能重组保留现有实现，新的 Terminal 参数显示另外提交。
