# Fleet Windows 分发与隔离

分支：`fleet/feat/portable`；集成发布分支：`main-fleet`。显示名 AO Fleet，程序 `fleet.exe`。支持 Windows x64 解压运行，可与官方 AO 并存；必须复制完整程序目录。数据独立保存在用户目录，不随程序移动，不自动导入官方或旧 Guardian 数据。

| 配置 | Fleet 默认值 |
| --- | --- |
| 状态根目录 | `%USERPROFILE%\.ao-fleet` |
| 数据库、工作区、托管账号 | 根目录下 `data` |
| Electron 配置、单实例锁 | 根目录下 `electron` |
| daemon 发现文件 / 端口 | 根目录下 `running.json` / `13001` |
| 自定义实例 | `AO_FLEET_HOME`（专用绝对路径）、`AO_FLEET_PORT`（空闲端口，不得为 3001/3002） |

旧版 `~/.ao/fleet` 不再作为默认路径或允许的自定义路径；升级前完全退出 Fleet，将数据迁至 `~/.ao-fleet`，同时检查持久化绝对路径及 Git worktree 关联。程序不自动移动数据，设置过 `AO_FLEET_HOME` 的环境也需更新。

Desktop 和随包 CLI 固定使用 Fleet 配置，不接受普通 `AO_DATA_DIR`、`AO_RUN_FILE`、`AO_PORT` 等继承值覆盖；不注册 `ao-app://`，关闭官方自动更新、版本下限检查和 feature build 切换。隔离仅覆盖 AO 自身状态，外部 Git/Codex/Claude 和手动选择的源码目录仍遵循各自配置。Cloud OAuth 沿用 localhost:3000 回调，未以真实账号验证。

构建：Node.js 24+、Git、满足 `go.work` 的 Go；首次联网准备依赖。在根目录依次运行 `npm ci --prefix packages/product-ui`、`npm ci --prefix frontend`、`npm --prefix frontend run package:fleet`。输出 `frontend/out/Fleet-win32-x64`、ZIP 和 SHA-256；不发布 Release，不强制运行测试。原有 package/make/publish 保持官方含义。

安装：在 `main-fleet` 工作副本双击 `scripts/install-fleet.cmd`，默认装到 `C:\ao`；也可调用 PowerShell 脚本并传 `-InstallDir`。它重新构建当前源码，验证 Fleet CLI 标记后整目录替换，修复桌面 AO Fleet 快捷方式，并将随包 CLI 放到用户 PATH 首位。不会拉代码或启动程序；构建失败保留旧安装。已开的终端/IDE 需重开以刷新 PATH。

安装目录必须为空或带 `.fleet-install.json` 管理标记；拒绝覆盖源码、数据、无关目录及链接，发现占用进程即报错，不强杀。程序目录不能放个人文件。可用不提交 Git 的 `scripts/install-fleet.local.json` 设置 `installDir`、`node`、`go` 绝对路径；显式命令行路径优先。系统 PATH 的 AO 冲突需自行处理。

退出：普通关闭窗口保留后台任务；顶部 `Fleet → 完全退出…` 经确认后只停止当前实例的 daemon、provider 和终端，保留历史与工作目录。失败保留窗口供重试。更新前使用完全退出，再替换程序目录，保留数据根目录。

验证入口：`scripts/install-fleet.test.ps1` 使用临时目录/桌面/PATH；`npm --prefix frontend run test:fleet:packaged` 检查真实产物隔离和退出。打包重建 better-sqlite3 为 Electron ABI 后，Node 数据库单测前需在 frontend 执行 `npm rebuild better-sqlite3`。

同步官方时核对新增硬编码 `.ao` 路径、最早启动顺序、环境覆盖、更新 IPC、Forge 资源及后台退出机制。Windows Codex 账号修复不属于本分支。
