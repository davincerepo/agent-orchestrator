# 将官方 AO 会话迁移到 AO Fleet

> 历史参考：本工具仅保留在 `fleet/feat/session-import`，不合入 `main-fleet`，不随 Fleet 包提供。以下流程对应 2026-09 的一次性同机迁移及 schema 139/140，未来版本不能直接照搬。

这是一次性、同机的会话接管工具。项目、AO 聊天历史、附件、工作区文件及未提交改动迁入 Fleet；迁移后由 Fleet 继续这些会话。两套程序不能同时恢复同一批会话。

工具独立于桌面主进程、Go daemon 和 API，不改数据库 schema、不增加设置页或多语言文案。无需重新打包 Fleet。代码在 `frontend/fork-fleet/migration.mjs`，入口是 `frontend/scripts/migrate-fleet.mjs`。

## 支持范围

- 同一台电脑、同一 Windows 用户；使用 Node.js 24+、Git、系统 PowerShell 和 robocopy。Node 内置 SQLite，不需要安装 npm 依赖。
- 当前支持 Codex **Chat** 会话；其他 agent 或 TUI 会话会被拒绝，避免把无法恢复的数据当成成功迁移。
- Fleet 已经启动过一次，存在初始化后的数据库，且尚无会话。两边数据库的表、索引和触发器定义必须一致，或仅相差 Fleet 的 `0140_model_parameters` 三列（原版版本 139 → Fleet 140）。后者在内存中演算并比较完整 schema，不修改原版；导入副本保留 139，由 Fleet 下次启动执行自己的 140 迁移。其他版本/结构差异、降级和合并两份历史均拒绝。
- Codex 原生会话仍使用当前用户的 `CODEX_HOME`（默认 `~/.codex`），不会复制、改写或清空它。未归档会话必须能找到原生 rollout 文件；已归档会话缺失原生文件时报告警告，保留 AO 历史，但不能承诺 Resume。
- 普通 linked worktree、多仓库项目及数据目录内额外的 linked worktree 一并处理。拒绝锁定、注册关系异常的 worktree、submodule `.git` 间接引用和断开的目录链接，需先人工处理。

默认源数据为 `~/.ao/data`，目标为 `~/.ao/fleet/data`。允许专用绝对目录，但拒绝目录别名、junction 映射和数据目录重叠。

## 使用

从 `frontend/` 运行，默认只检查，不修改数据：

```powershell
node scripts/migrate-fleet.mjs
# 或 npm run migrate:fleet
```

检查通过后，在原版结束正在进行的 turn，退出 agent（保留会话和 worktree），关闭两套桌面和残留 AO host。关闭窗口不一定结束后台 agent。工具只检测并拒绝运行中的进程，不自动强制结束任务。

```powershell
node scripts/migrate-fleet.mjs --apply
```

可选参数：

```powershell
node scripts/migrate-fleet.mjs --source-root C:\Users\z\.ao --fleet-root D:\FleetData --codex-home C:\Users\z\.codex
```

`--fleet-root` 默认为 `AO_FLEET_HOME` 或 `~/.ao/fleet`；它与 `--source-root` 都填写包含 `data` 的状态根目录。迁移期间保持两套应用关闭。数据较多时复制会持续数分钟，并额外占用一份数据和一份数据库备份的空间。

完成后启动原来的 `fleet.exe`，检查项目、历史消息、文件差异，再 Resume。原版暂时保持关闭：它的原数据还在，但 Git worktree 注册已经交给 Fleet，不能把它当作另一个可并行操作的副本。

## 迁移与回滚机制

1. 只读检查数据库兼容性、未结束 turn、原生会话、文件链接与 Git 注册。
   应用和回滚通过独立 `migration-lock.db` 的排他锁互斥；进程异常退出会自动释放锁。
2. 在 `<FleetRoot>/migrations/<时间>/` 保存迁移日志及 SQLite Backup API 产生的原数据库快照，包含已提交的 WAL 数据。保留原版数据文件。
3. 将数据复制到暂存目录。Windows 保留文件 ACL，复制链接本身而不遍历链接目标；不复制 `runtime`、移动端运行状态、遥测标识、SQLite WAL/SHM 或根目录的 `running.json`、Electron 配置。
4. 在副本中改写数据库的绝对路径字段和结构化 JSON 的路径字段。外部源码仓库路径、原生 thread ID、消息正文及历史命令原样保留；清除旧终端记录、运行句柄、控制器代号和预览地址，未归档会话标为 exited，等待 Fleet 恢复。
5. 原 Fleet `data` 改名保存在迁移目录，暂存数据安装到目标；重建目录链接，并用 `git worktree repair` 更新全部 linked worktree 注册。每一步 Git 修改前写入日志。

应用阶段失败会尝试自动回滚。断电或进程异常退出后，使用日志路径恢复：

```powershell
node scripts/migrate-fleet.mjs --rollback 'C:\Users\z\.ao\fleet\migrations\<时间>\migration.json'
```

回滚前仍须关闭两套应用与 AO host。回滚恢复原 Git 注册和旧 Fleet 数据目录；迁移后的目录改名保留在日志旁，不递归删除用户数据。原版数据库和工作区文件本身一直保留。

回滚适用于迁移验证期间。若迁入 Fleet 后已经开展新工作，新消息和文件不会自动合并回原版；保留目录用于找回这些新增内容。不要删除迁移日志、原版数据或保留目录，直到确认迁移结果。

## 验证与维护

```powershell
node --test fork-fleet/migration.node-test.mjs
```

测试使用独立临时 Git 仓库和 SQLite 数据库，覆盖未提交文件、嵌套 worktree、目录链接、历史文本保持、附件路径、运行状态清理、WAL 备份、部分完成日志回滚，以及活跃进程、非空目标、schema 差异、缺失原生历史和路径别名的拒绝行为。另覆盖原版 139 导入 Fleet 140、额外索引或未知版本被拒绝，以及回滚恢复原来的 140 数据库。

本次在 Windows 上完成真实迁移：21 个会话、86 个 linked worktree、17 个目录链接；迁移前后 2,317 条消息正文的 SHA-256 一致，项目、会话、turn 和 provider event 数量一致。Fleet 启动后 12 个未归档会话均恢复为 `ready`，原生 thread ID 全部保持不变。另有 2 个已归档会话在迁移前就缺少原生 rollout，仅验证其 AO 历史保留。恢复验证没有发送新的业务消息。

未来同步上游时检查：数据目录新增持久文件、数据库新增路径/JSON 字段、会话运行状态字段和 Codex 原生文件布局。schema 一致性检查只保证存储定义相同，不能替代真实 Resume 验证。

实现依据：[SQLite Backup API](https://nodejs.org/docs/latest-v24.x/api/sqlite.html#sqlitebackupsource-db-path-options)、[Git worktree repair](https://git-scm.com/docs/git-worktree)、[robocopy 的文件安全信息及链接复制选项](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/robocopy)。
