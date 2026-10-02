# AO Fleet fork

上游：https://github.com/Untrivial-ai/agent-orchestrator · Fork：https://github.com/davincerepo/agent-orchestrator

`main-fleet` 是日常同步官方 main 和集成本地功能的入口。`fleet/feat/common` 仅放共用代码、配置和维护约定；只有本地功能依赖官方新增代码时，才将官方合入 common。Windows Codex 账号使用官方实现。

## 功能分支

| 分支 | 范围 | 说明文件（合入后提供） |
| --- | --- | --- |
| `fleet/feat/agent-commands` | 项目查询与派单范围、1 MiB 消息；steer 使用官方实现 | [agent-commands.md](agent-commands.md) |
| `fleet/feat/model-parameters` | 官方 Effort、Codex Fast、参数恢复与反显 | [model-parameters.md](model-parameters.md) |
| `fleet/feat/portable` | 免安装、数据与进程隔离、AO Fleet 品牌 | [fleet-portable.md](fleet-portable.md) |

`fleet/feat/session-import` 仅保留历史参考，不参与集成。

## 分支维护约定

- 实现先提交到所属功能分支，再合入 `main-fleet`；功能分支保持独立，不反向合入集成分支。
- 日常同步：备份 `main-fleet`，将官方 main 直接合入 `main-fleet`，在集成处解决冲突并验证；不向 common 和所有功能分支同步官方。
- 依赖例外：某个功能确实需要官方新增 API、字段或代码时，才先将官方合入 `fleet/feat/common`，再将 common 合入相关功能分支；无关分支保持原状。本次模型参数收敛到官方 Session Effort 属于此例外。
- 功能修正提交到所属功能分支，再合入 `main-fleet`。官方已经提供的功能应从本地差异中移除，只保留扩展；不要重放旧补丁重新引入重复字段或行为。旧提交保留在历史中不会自行重放，后续以合并后的共同祖先计算差异。
- 外部修复 PR 保留原提交：将 PR head 拉到单独的本地 `codex/fix-*` 分支，直接合入 `main-fleet`，不为改名而重写提交。
- 临时 worktree 使用完释放。每个功能分支只维护一份简短说明，记录行为、边界和升级注意事项，不列文件清单或复述提交历史。

## 换行规则

官方源文件使用 LF。本地 `.gitattributes` 固定源代码、SQL、配置和文档为 LF，`.editorconfig` 提醒编辑器按同样规则保存。每个新 clone 应运行下面的仓库级配置，不修改全局设置：

```sh
git config --local core.autocrlf input
git config --local merge.renormalize true
```

`merge.renormalize` 会在三方合并前按属性重新归一化内容，避免旧 CRLF 提交把真实的小改动扩大成整文件冲突。首次引入规则时检查 `git ls-files --eol`，对受规则约束的文件执行 `git add --renormalize` 并审查差异。它只消除换行造成的假冲突；双方修改同一段逻辑时仍需解决实际冲突。
