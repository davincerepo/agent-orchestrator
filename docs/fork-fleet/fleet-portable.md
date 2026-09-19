# Fleet Windows 免安装包与数据隔离

## 使用方式

运行 `npm run package:fleet` 会生成 **Windows x64 完整应用目录和 ZIP**。解压后双击 `Fleet-win32-x64/fleet.exe`，无需安装，不需要目标电脑预装 Node.js 或 Go。`fleet.exe` 必须和同目录的 DLL、`resources/` 等文件放在一起，不能单独复制。

可与已安装的官方 AO 同时运行。Fleet 首次启动使用自己的数据，不会自动复制官方的项目、会话或账号配置。程序目录可以移动；默认数据保存在用户目录，**并不是把数据也放在程序旁边的随身版**。

历史一次性导入工具仅保留在 `fleet/feat/session-import` 参考分支，不随主分支和安装包提供。

更新可以手动解压新版，或使用下述固定目录安装脚本；保留数据目录。沿用上游的持久终端机制，关闭窗口后 agent/终端可能继续占用后台程序，替换程序目录前需结束这些任务。Fleet 隐藏设置中的 Updates 入口，不启动自动更新、官方版本下限检查或 feature build 切换。更新说明集中在本文和包内 `README-Fleet.txt`。

窗口名称和 Help → About 显示 **AO Fleet**，由 `profile.json` 的 `displayName` 配置。发行名 `Fleet`、`fleet.exe`、包名、数据目录和配置关键字保持不变。侧栏、启动页、托盘和原有多语言文件保持上游实现，没有额外的 Fleet 更新说明页面。

本地维护分支为 `main-fleet`，功能开发分支为 `fleet/feat/portable`。提交后不自动推送；本地分支更名不会修改远端分支或历史提交。

## 本地构建

在 Windows x64 使用 Node.js 24+、npm、Git，以及能满足根目录 `go.work` 的 Go 工具链（当前为 1.26.5）。Go 的自动工具链下载也可满足这个要求。首次构建需要联网下载依赖、Electron 和打包所需的运行时；沿用上游的原生依赖构建步骤。

从仓库根目录执行 PowerShell：

```powershell
npm ci --prefix packages/product-ui
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
npm ci --prefix frontend
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Set-Location frontend
npm run package:fleet
```

构建输出（版本号跟随 `frontend/package.json`）：

```text
frontend/out/
  Fleet-win32-x64/
    fleet.exe
    README-Fleet.txt
    resources/
      app.asar
      daemon/ao.exe
      acp-runtime/
      ...
    ...
  Fleet-<version>-win32-x64.zip
  Fleet-<version>-win32-x64.zip.sha256
```

构建脚本准备资源、编译、打目录包并压缩，**不强制运行测试**，也不会创建安装器或发布 GitHub Release。原有 `package`、`make`、`publish` 命令保持上游含义；本功能使用单独的 `package:fleet` 入口。

如果机器装有多个 Node.js，确认 `node --version` 和 npm 实际使用的 Node 都是 24+。直接调用另一目录的旧 `npm.cmd` 可能仍会使用旁边的旧 Node。

## 固定目录安装与更新（Windows）

在 **`main-fleet` 工作副本**中双击 `scripts/install-fleet.cmd`。默认安装到 `C:\ao`，安装后桌面的 **AO Fleet** 快捷方式直接启动 `C:\ao\fleet.exe`，工作目录和图标也指向该目录；每次安装都会重新创建这个快捷方式，修复目标错误、参数残留、损坏或被删除的情况。通过 Windows 获取当前用户的实际桌面目录，支持 OneDrive/重定向桌面，不修改其他快捷方式。

流程：检查分支、目标目录、运行进程和命令行冲突 → `npm ci` 准备 product-ui/frontend 依赖 → 调用现有 `package-fleet.mjs` 重新打包 → 验证包内 CLI 的 Fleet 构建标记 → 复制本次 `frontend/out/Fleet-win32-x64` 目录到临时目录 → 再次检查运行进程 → 替换目标目录 → 修复快捷方式 → 注册默认 `ao` 命令。不会根据 ZIP 文件时间挑选旧包，不自动拉取代码、切换分支或启动应用；未提交的源码修改也会参与构建。构建失败不改变已安装程序和 PATH。

安装成功后，`resources/daemon` 位于当前用户 PATH 首位，原 AO 默认安装目录的 CLI 条目及旧的受管 Fleet CLI 条目会从用户 PATH 中移除，其他工具条目和官方程序文件保留。脚本更新自身进程的 PATH，并向 Windows 广播环境变化；其他已打开的终端仍须重新打开，长期运行的 IDE 可能需要重启。若系统级 PATH 中已有其他 `ao`，脚本在构建前报出冲突，不修改系统级环境；先移除该系统级 AO PATH 条目再安装。PowerShell 中手工定义的 `ao` alias/function 不属于 PATH，需自行移除。

`ao -v`、`ao --version` 和 `ao version` 均显示 `AO Fleet ...`，安装脚本通过 `ao -v` 验证构建标识。Fleet 包内的 `ao.exe` 通过构建标记自行固定数据目录、run-file 和端口，不依赖从 Desktop 继承环境。默认连接 `.ao/fleet` 和 `13001`；自定义实例继续使用 `AO_FLEET_HOME` / `AO_FLEET_PORT`，忽略继承的普通 `AO_DATA_DIR` / `AO_RUN_FILE` / `AO_PORT`。`ao start` 只打开同一安装包的 `fleet.exe`；文件缺失时报错，不扫描、下载或启动官方 AO。

首次安装要求目标不存在或为空。脚本写入 `.fleet-install.json` 标记，只更新自己管理的目录，拒绝覆盖已有的无关目录、源码、默认/当前 `AO_FLEET_HOME` 数据目录及 junction/符号链接。自定义安装路径应专用于程序文件；不要把数据、项目或个人文件放入其中。升级完整替换程序目录，过时文件也会清理。临时旧目录仅用于切换失败时恢复，成功后删除，不积累历史版本。若清理因文件占用失败，会报告错误，保留临时目录以便处理。

脚本检查目标目录中运行的所有可执行文件，包括 Fleet UI、daemon 和 terminal host。发现占用时报告名称和 PID 并停止，不强杀进程；关闭窗口可能不够，需要先结束相应后台任务。退出后重新运行脚本。官方 AO 安装在其他目录，不会因此被关闭。默认数据继续位于 `%USERPROFILE%\.ao\fleet`，程序替换不迁移或清理数据。

命令行使用（不暂停窗口，可更改安装目录）：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install-fleet.ps1
# 或安装到另一专用目录
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install-fleet.ps1 -InstallDir D:\Apps\AO-Fleet
```

依赖同上：Node.js 24+、npm、Go 满足 `go.work`、Git。双击时使用 PATH；若本机默认版本太旧，可创建 **不提交 Git** 的 `scripts/install-fleet.local.json`，指定现有工具的绝对路径，仅对本次构建生效，不修改系统 PATH。可选的 `installDir` 指定双击时的默认安装位置，命令行 `-InstallDir` 优先；例如 `C:\ao` 已有数据时使用 `C:\ao\Fleet` 子目录，保留旁边的数据：

```json
{
  "installDir": "C:\\ao\\Fleet",
  "node": "C:\\Tools\\node24\\node.exe",
  "go": "C:\\Tools\\go\\bin\\go.exe"
}
```

Windows 安装脚本测试：`powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install-fleet.test.ps1`。测试使用临时安装目录、临时桌面和假的用户 PATH 存储，验证首次安装、整包更新、快捷方式修复、占用拒绝、目录保护、构建失败、CLI 优先级、迁移与重复安装，不修改真实桌面、持久 PATH 或 AO 数据。CLI 隔离和启动选择另由 `go test ./internal/cli` 覆盖。

## 隔离边界

| 项目 | 官方 AO 默认值 | Fleet 默认值 |
| --- | --- | --- |
| 可执行程序 | `agent-orchestrator.exe` | `fleet.exe` |
| 应用显示名 | Agent Orchestrator | AO Fleet |
| Windows App ID | `dev.agent-orchestrator.desktop` | `dev.fleet.agent-orchestrator` |
| 状态根目录 | `%USERPROFILE%\.ao` | `%USERPROFILE%\.ao\fleet` |
| Electron 配置、缓存、Cookie、单实例锁 | `.ao\electron` | `.ao\fleet\electron` |
| 数据库、工作区、托管账号等后端数据 | `.ao\data` | `.ao\fleet\data` |
| 后台进程发现文件 | `.ao\running.json` | `.ao\fleet\running.json` |
| 后台服务端口 | `3001` | `13001` |
| Agent Browser 状态 | `.ao\br` | `.ao\fleet\br` |
| Cloud 登录状态、应用状态、更新设置 | `.ao` 下相应文件 | `.ao\fleet` 下相应文件 |
| `ao-app://` 协议注册 | 官方应用管理 | 不注册、不覆盖 |
| 应用更新 | 官方自动更新 | 退出后手动更换整个程序目录 |

Fleet 在 Electron 单实例锁和读取 Cloud/遥测环境前固定自己的路径，后台进程的工作目录也指向 Fleet 根目录。Windows 登录 shell 环境不能覆盖这些值。前端等待本应用提供的后台服务地址，不接受构建环境里的 `VITE_AO_API_BASE_URL` 覆盖。

从官方 AO 的终端启动 Fleet 时，继承的 `AO_DATA_DIR`、`AO_RUN_FILE`、`AO_PORT`、`AO_DAEMON_COMMAND`、`AO_DEV_DAEMON_BINARY` 都不会改变 Fleet 的隔离配置。构建标记写入 main 和 renderer，运行时设置 `AO_DESKTOP_FLAVOR` 不能取消隔离。

高级选项（启动前设置）：

```powershell
$env:AO_FLEET_HOME = 'D:\FleetData'
$env:AO_FLEET_PORT = '13002'
& '.\Fleet-win32-x64\fleet.exe'
```

`AO_FLEET_HOME` 必须是专用绝对路径；常见官方目录 `.ao`、`.ao/data`、`.ao/electron` 等会被拒绝。不要选其他 AO 实例的自定义数据目录，也不要通过 junction/符号链接映射到它。`AO_FLEET_PORT` 必须是空闲端口，范围 1–65535，禁止使用官方默认的 3001/3002。只改数据根目录而不改端口，不能同时运行两个 Fleet 数据实例；同一个 Fleet 配置仍然维持单实例。

旧 Guardian 测试版的 `.ao/guardian` 数据不会被自动移动或删除。需要沿用时，先退出旧版及其任务、备份旧数据，再将数据迁移至独立的 `.ao/fleet` 目录；不要让两个版本共享同一份数据。旧的 `AO_GUARDIAN_*` 环境变量不再作为 Fleet 配置使用。

隔离的是 AO 自己的应用状态，并非操作系统沙箱：手动添加同一个源码目录时仍会访问该目录；外部 Git、Codex、Claude 等工具仍遵循各自的配置。应用启动时不会自动迁移官方 AO 数据；历史导入工具仅供参考，不作为通用升级步骤。新的 Fleet 构建中可从外部终端直接运行包内 `resources/daemon/ao.exe`，它与 Desktop 使用相同隔离配置；此前自报 `dev`、未带 Fleet 构建标记的旧 CLI 仍依赖 Desktop 环境，应重建升级。

Cloud 登录复用现有的 `http://127.0.0.1:3000/callback` 回环 OAuth 路径，避免抢占官方 `ao-app://`。本次未使用真实账号验证 Cloud OAuth 登录；本地项目和 agent 使用不依赖此验证。

## 实现位置与同步上游

- `frontend/fork-fleet/profile.json`：应用身份、默认端口、构建标记和 fork 更新元数据。
- `frontend/fork-fleet/{forge,build,runtime}.ts`：Forge 配置覆盖层、Vite 编译标记、运行时隔离配置。
- `frontend/src/main/fleet-bootstrap.ts`：最早初始化数据路径，并包装原有 daemon launch resolver。
- `frontend/src/main.ts`：少量条件接入，覆盖数据目录、daemon 环境、协议与更新入口；上游默认分支行为不变。
- `frontend/src/shared/desktop-flavor.ts`：main/renderer 共用的编译开关与显示名称。
- `frontend/src/renderer/components/settings/settingsCatalog.tsx`：通过现有可见性规则隐藏 Updates 入口，复用原有设置组件和翻译。
- `frontend/scripts/package-fleet.mjs`：复用上游资源准备脚本，生成完整目录、ZIP 和 SHA-256。
- `frontend/scripts/smoke-fleet.mjs`：对真实打包产物执行临时用户目录测试。
- `scripts/install-fleet.{cmd,ps1}`：从集成分支重新打包、替换固定安装目录并修复桌面快捷方式；`install-fleet.test.ps1` 验证安装边界。

这是一项独立的桌面分发功能，与 Windows Codex 账号存储修复、CI 编译配置分别提交。不改 Go 后端或 API。未来 rebase 时重点检查：新增的硬编码 `.ao` 路径、Electron 初始化顺序、daemon 环境覆盖顺序、自动更新入口，以及 Forge hooks/资源列表变化。配置覆盖层和独立脚本尽量复用上游机制。

原有品牌展示组件及 8 个语言文件的改动已撤回。Forge 直接沿用上游 hooks，不再重复写更新元数据；构建脚本仍将 `AO_RELEASE_REPO` 指向 fork。保留所有数据、进程和更新隔离检查以及对应的测试。

## 验证

在 `frontend/` 运行：

```powershell
npm run typecheck
npm test -- fork-fleet src/renderer/components/settings/UpdatesSection.test.tsx src/renderer/components/SettingsDialog.test.tsx src/renderer/components/GlobalSettingsForm.test.tsx src/renderer/lib/api-client src/main/tray.test.ts src/renderer/components/Sidebar.test.tsx src/renderer/components/DaemonStartupLoader.test.tsx src/renderer/i18n
npm run package:fleet
npm run test:fleet:packaged
```

smoke 使用临时 `USERPROFILE`、独立端口 13011 和空账号目录；故意注入指向官方 AO 的环境变量，验证仍使用 Fleet 自身路径和随包后台程序。检查后台 ready、真实设置页面隐藏 Updates、窗口名称和 Help → About 内容、所有更新命令不可启用自动更新、官方 3001 后台 PID 和协议注册保持不变，以及退出仅关闭 Fleet 的后台进程。结果 JSON、日志和截图保存在 `frontend/out/fleet-smoke/`，可通过 `AO_FLEET_TEST_ROOT` 更换测试输出目录。官方 AO 未运行时报告会明确标记，因此双开验证应在官方 AO 已运行时执行。

本次精简和更名通过了类型检查、231 项相关回归测试、真实 Windows x64 打包以及 ZIP 解压后与官方 AO 双开的 smoke，确认窗口名称、About 内容和 Updates 入口行为符合上文。初版曾在 Windows 上运行完整 frontend suite，仍有未解决的测试失败/环境缺口：macOS 路径、Unix socket/权限/可执行位、未安装的独立 landing 依赖，以及两个聊天日期标签断言。完整 suite 不作为“全部通过”报告，macOS/Linux CI 尚未验证。

注意：上游 Forge hook 会把源目录的 `better-sqlite3` 重建为 Electron ABI。打包后再用 Node 24 跑数据库相关单测前，执行 `npm rebuild better-sqlite3` 恢复 Node ABI；下一次打包会重新准备 Electron ABI。此次恢复后浏览器配置导入相关测试通过。
