# AO Fleet fork

上游：https://github.com/Untrivial-ai/agent-orchestrator · Fork：https://github.com/davincerepo/agent-orchestrator

`fleet/feat/common` 是同步官方 main 的公共基底，仅放共用代码、配置和维护约定；`main-fleet` 集成下列功能，不额外维护 Windows Codex 账号补丁，使用官方实现。

## 功能分支

| 分支 | 范围 | 说明文件（合入后提供） |
| --- | --- | --- |
| `fleet/feat/agent-commands` | 项目查询与派单范围、1 MiB 消息；steer 使用官方实现 | [agent-commands.md](agent-commands.md) |
| `fleet/feat/model-parameters` | 官方 Effort、Codex Fast、参数恢复与反显 | [model-parameters.md](model-parameters.md) |
| `fleet/feat/portable` | 免安装、数据与进程隔离、AO Fleet 品牌 | [fleet-portable.md](fleet-portable.md) |

`fleet/feat/session-import` 仅保留历史参考，不参与集成。

## 分支维护约定

- 实现先提交到所属功能分支，再合入 `main-fleet`；功能分支保持独立，不反向合入集成分支。
- 官方更新先合入 `common`，各功能分支适配后再集成；跨功能冲突较多时应调整分支结构并反馈。
- 临时 worktree 使用完释放。每个功能分支只维护一份简短说明，记录行为、边界和升级注意事项，不列文件清单或复述提交历史。
