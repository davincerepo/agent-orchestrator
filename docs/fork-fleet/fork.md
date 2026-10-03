# AO Fleet fork

上游：https://github.com/Untrivial-ai/agent-orchestrator · Fork：https://github.com/davincerepo/agent-orchestrator

`main-fleet` 是日常同步官方 main 和集成本地功能的入口。`fleet/feat/common` 仅放共用代码、配置和维护约定；只有本地功能依赖官方新增代码时，才将官方合入 common。

## 功能分支

| 分支 | 范围 | 说明文件（合入后提供） |
| --- | --- | --- |
| `fleet/feat/agent-commands` | 项目查询与派单范围、1 MiB 消息；steer 使用官方实现 | [agent-commands.md](agent-commands.md) |
| `fleet/feat/model-parameters` | 官方 Effort、Codex Fast、参数恢复与反显 | [model-parameters.md](model-parameters.md) |
| `fleet/feat/portable` | 免安装、数据与进程隔离、AO Fleet 品牌 | [fleet-portable.md](fleet-portable.md) |

`fleet/feat/session-import` 仅保留历史参考，不参与集成。

## 分支维护约定

- 实现先提交到所属功能分支，再合入 `main-fleet`；功能分支保持独立，不反向合入集成分支。
- 日常同步：备份 `main-fleet`，将官方 main 直接合入 `main-fleet`，在集成处解决冲突并验证；不向 common 和所有功能分支同步官方。common分支有提交后要同步到所有功能分支（已不参与集成分支的除外）。
- 依赖例外：某个功能确实需要官方新增 API、字段或代码时，才先将官方合入 `fleet/feat/common`，再将 common 合并到所有功能分支；
- 合并时要报告冲突情况：官方 main 直接合入 `main-fleet`时，分类统计各种冲突的数量，比如在同一个位置两边都增加字段的，选择双方都保留，最容易解决；同一个方法两边都增加参数的，需要合并参数。还有需要特殊处理的，根据实际功能合并逻辑，最难处理；归类后报告一下，等用户回复再继续合并。难处理的合并增多时要研究一下解决方案，避免每次合并重复解决冲突。比如同一个功能本地先引入后官方也引入了，那么会出现大量冲突，这种情况要去掉本地的引入，改用官方的。
- 功能修正提交到所属功能分支，再合入 `main-fleet`。官方已经提供的功能应从本地差异中移除，只保留扩展；不要引入重复字段或行为。
- 外部修复 PR 保留原提交：将 PR head 拉到单独的本地 `codex/fix-*` 分支，直接合入 `main-fleet`，不为改名而重写提交。
- 临时 worktree 使用完释放。每个功能分支只维护一份简短说明，记录行为、边界和升级注意事项，不列文件清单或复述提交历史。

## 换行规则

官方源文件使用 LF。本地 `.gitattributes` 固定源代码、SQL、配置和文档为 LF，`.editorconfig` 提醒编辑器按同样规则保存。每个新 clone 应运行下面的仓库级配置，不修改全局设置：

```sh
git config --local core.autocrlf input
git config --local merge.renormalize true
```

`merge.renormalize` 会在三方合并前按属性重新归一化内容，避免旧 CRLF 提交把真实的小改动扩大成整文件冲突。首次引入规则时检查 `git ls-files --eol`，对受规则约束的文件执行 `git add --renormalize` 并审查差异。它只消除换行造成的假冲突；双方修改同一段逻辑时仍需解决实际冲突。

## 约束
- 此文档是为了仓库的长期维护而存在的，里面的写的规则是从不依赖某次任务的背景就能看懂的。换句话说，一次临时任务中的要求而不是长期规则的不要写入文档，不然下次重新看文档没头绪的。
