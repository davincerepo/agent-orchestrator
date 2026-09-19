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

所有功能与修复都必须先切换到对应的 `fleet/feat/*` 或 `fleet/fix/*` 分支完成并提交，再合并到 `main-fleet`，禁止在 `main-fleet` 直接提交实现。

功能分支从公共基底分叉，不合入无关功能；同一功能内保留独立的实现、修复和 CI 提交。每项功能保留实现文档与行为测试。`main-fleet` 通过显式 merge commit 集成已验证分支，用于运行和打包。

使用独立 Git worktree 开发时，工作完成后必须释放该 worktree 对功能分支的占用，避免主工作目录无法切换到同一分支。先确认修改已提交或妥善保存、没有仍在使用该 worktree 的开发任务，再执行 `git -C "<worktree 路径>" switch --detach`，保留目录和提交但解除分支占用；确认目录不再需要时，也可以使用 `git worktree remove "<worktree 路径>"` 清理。不得强制删除有未保存修改的 worktree。收尾时用 `git worktree list` 确认已完成任务的独立 worktree 不再占用功能分支。

同步官方时先更新公共基底，再逐个更新功能分支并在临时集成分支验证。不要将整个 `main-fleet` 合回单个功能分支。共享 API/SQL 生成文件根据各分支实际源代码重新生成，集成时再校验。不修改已经应用的数据库迁移，不把同步上游与不相关功能变更混在一个提交。
