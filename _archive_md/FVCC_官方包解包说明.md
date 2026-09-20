
# FVCC 官方包解包说明

> 本文基于 `D:\Fnos.VideoConversion\FVCC_official_unpack\` 目录的**实际解包结果**逐项核对撰写，全部数据（文件清单、体积、清单字段、脚本内容）均取自该目录真实文件，未纳入包外推测信息。

## 文档说明

- **核心内容**：说明官方 FVCC FPK 安装包的容器格式与解包方式、解包后目录结构逐项含义、前端源码组织方式、关键技术约定，以及与自研 FVCC 项目（`D:\Fnos.VideoConversion\FVCC`）的关系。
- **目标读者**：自研 FVCC 项目的开发者，用于对照官方包结构、复用官方前端源码、校准打包产物。
- **全文数据基准**：解包目录 `D:\Fnos.VideoConversion\FVCC_official_unpack`。

---

## 一、包来源与容器格式

| 项目 | 实际值 |
|------|--------|
| 原始包路径 | `D:\Download\FVCC_v1.0_fnos_x86.fpk` |
| 包体积 | 22.6 MB |
| 容器格式 | gzip（文件头魔数 `1F 8B 08 00`），即 **tar.gz 归档**，非专有二进制封装 |
| 外层归档成员数 | 18 项 |
| 解包结果 | 2794 个文件 / 375 个目录 / 102.24 MB |
| 剔除 `node_modules` 后 | 46 个文件 / 12 个目录 / 44.02 MB |

**解包方式**：因 FPK 实为 gzip 流，按文件魔数判定为 `tar.gz` 后直接用 tar 解出外层内容；外层归档中另有一个二级归档 `app.tgz`（23,632,079 B），需再解一次，得到 `ui/`（生产前端）与 `ui-src/`（前端源码）两部分。当前解包目录顶层的 `ui/` 与 `ui-src/` 即来自该二级归档展开结果，`app.tgz` 原文件仍保留在顶层。

> 二级归档的存在意味着**包内文件是"归档套归档"的两层结构**：若后续需要重新打包或校验，需按同样层级还原（外层 tar.gz + 内层 app.tgz），不能将解包结果直接拍平。

---

## 二、目录结构逐项说明

顶层 12 项（7 个目录 + 5 个文件）构成完整包结构：

| 名称 | 类型 | 体积 | 作用 |
|------|------|------|------|
| `manifest` | 文件 | 478 B | 应用元数据清单，FNOS 应用商店/安装器据此注册应用 |
| `ICON.PNG` | 文件 | 6483 B | 应用图标（64 级），PNG 魔数校验通过 |
| `ICON_256.PNG` | 文件 | 58595 B | 应用图标（256 级） |
| `app.tgz` | 文件 | 23.6 MB | 二级归档，内含 `ui/`（生产前端）与 `ui-src/`（前端源码） |
| `fvcc` | 文件 | 21,971,128 B（21.9 MB） | 核心二进制，ELF 64-bit LSB、机器码 62（x86-64），面向 Linux x86_64 平台，**Windows 环境不可直接运行** |
| `cmd/` | 目录 | 9 个脚本 | 生命周期脚本，安装器在各阶段回调调用 |
| `config/` | 目录 | 2 个文件 | 权限与资源共享声明 |
| `ui/` | 目录 | 8 个文件（含隐藏文件） | 生产前端产物，由后端直接托管 |
| `ui-src/` | 目录 | 2770 个文件 | 前端完整源码 + 依赖，**官方包内含源码，无需逆向** |
| `server/` | 目录 | 0 个文件 | 保留位，无内容 |
| `wizard/` | 目录 | 0 个文件 | 保留位，无内容 |
| `www/` | 目录 | 0 个文件 | 保留位，无内容 |

### 2.1 manifest（478 B）

```
appname               = fvcc
version               = 1.0.0
display_name          = FVCC
desc                  = Fnos Video Conversion Client - 视频转码任务调度与远程服务器管理
arch                  = x86_64
source                = thirdparty
maintainer            = fvcc
distributor           = fvcc
desktop_uidir         = ui
desktop_applaunchname = fvcc.Application
platform              = 
checksum              = a4d90f96ad8e83f9db5df5bdfae41392
```

关键字段含义：`desktop_uidir = ui` 指明 Web 界面资源目录；`desktop_applaunchname = fvcc.Application` 是应用入口标识，**必须与 `ui/config` 中 `.url` 的键名完全一致**，二者通过该字符串绑定。

### 2.2 cmd（9 个 bash 生命周期脚本）

| 脚本 | 体积 | 内容要点 |
|------|------|----------|
| `main` | 2867 B | 唯一有实体逻辑的脚本，实现 `start` / `stop` / `status` 三个动作 |
| `config_init` | 126 B | 用户修改应用环境变量**前**回调，仅 `exit 0` |
| `config_callback` | 511 B | 用户修改环境变量**后**回调，把 `TRIM_DATA_ACCESSIBLE_PATHS` 写入 `${TRIM_PKGVAR}/accessible_paths.env`，使运行中的应用无需重启即可识别新授权目录 |
| `install_init` / `install_callback` | 88 B / 87 B | 安装前 / 安装后回调，均空实现 |
| `upgrade_init` / `upgrade_callback` | 88 B / 87 B | 升级前 / 升级后回调，均空实现 |
| `uninstall_init` / `uninstall_callback` | 89 B / 89 B | 卸载回调，均空实现（两个脚本的注释文字相同，原包即如此） |

`cmd/main` 的核心机制：

- 运行态文件：日志 `${TRIM_PKGVAR}/info.log`、PID `${TRIM_PKGVAR}/app.pid`、Unix Socket `${TRIM_APPDEST}/app.sock`；
- 启动时以三个环境变量拉起二进制：`FVCC_SOCK`（socket 路径）、`FVCC_UIDIR`（指向 `ui` 目录）、`FVCC_DATADIR`（指向 `TRIM_PKGVAR` 数据目录），并透传 `TRIM_DATA_ACCESSIBLE_PATHS`；
- 启动前清理残留 socket，并落盘 `accessible_paths.env`；停止时先 `TERM` 等待最长 10 秒，超时补 `KILL`，随后清理 PID 与 socket 文件。

### 2.3 config（权限与资源共享）

| 文件 | 体积 | 内容 |
|------|------|------|
| `privilege` | 59 B | `defaults.run-as = package`，即进程以包专用身份运行 |
| `resource` | 544 B | `data-share.shares` 声明两个共享：`fvcc` 与 `fvcc/data`，两者权限均为 `rw: [fvcc]` |

### 2.4 ui（生产前端产物，8 个文件）

| 路径 | 体积 | 说明 |
|------|------|------|
| `ui/index.html` | 930 B | 生产入口页，`<title>视频转码</title>` |
| `ui/assets/index-84EeMQNW.js` | 102,123 B | 打包压缩产物（含 Vite 内容哈希） |
| `ui/assets/index-e9ChqpP5.css` | 23,530 B | 打包压缩样式 |
| `ui/config` | 338 B | FNOS 桌面入口声明（见 2.5） |
| `ui/images/icon_64.PNG` | 6483 B | 界面图标占位 `icon_{0}.png` 的 64 级 |
| `ui/images/icon_256.png` | 58595 B | 256 级图标 |
| `ui/.DS_Store`、`ui/images/.DS_Store` | 6148 B ×2 | macOS 目录元数据残留，无功能作用 |

生产 `index.html` 的关键点：

- 头部内联脚本读取 `localStorage['fnos-theme-mode']`（`20` = 暗色、`10` = 亮色），否则回退 `prefers-color-scheme`，据此设置 `data-theme`；
- 以绝对前缀引用资源：`/app/fvcc/assets/index-84EeMQNW.js` 与 `/app/fvcc/assets/index-e9ChqpP5.css`，即**生产环境必须挂在 `/app/fvcc/` 路径下**。

### 2.5 ui/config（网关入口声明）

```json
{
    ".url": {
        "fvcc.Application": {
            "title": "FVCC",
            "icon": "images/icon_{0}.png",
            "type": "iframe",
            "protocol": "",
            "gatewayPrefix": "/app/fvcc",
            "gatewaySocket": "app.sock",
            "url": "/app/fvcc",
            "allUsers": true
        }
    }
}
```

该文件与 `manifest.desktop_applaunchname` 成对：入口以 **iframe** 形式嵌入 FNOS 桌面，网关前缀 `/app/fvcc`，Socket 名为 `app.sock`（与 `cmd/main` 中 `${TRIM_APPDEST}/app.sock` 对应），图标按 `images/icon_{0}.png` 取不同尺寸。

---

## 三、前端源码组织（ui-src）

### 3.1 技术栈与工程配置

`ui-src/package.json` 显示该前端为 **无框架的纯 TypeScript + Vite + Tailwind** 项目，`dependencies` 为空对象，全部依赖均为构建期工具：

| devDependency | 版本 |
|---------------|------|
| vite | ^6.0.5 |
| typescript | ^5.7.2 |
| tailwindcss | ^3.4.17 |
| postcss | ^8.4.49 |
| autoprefixer | ^10.4.20 |
| @types/node | ^22.10.0 |

工程脚本：`dev = vite`、`build = tsc -b && vite build`、`preview = vite preview`。

其余配置文件的约定：

| 文件 | 关键内容 |
|------|----------|
| `vite.config.ts` | `base: '/app/fvcc/'`；`@` 别名指向 `src`；`build.outDir = '../ui'` 且 `emptyOutDir: false`（直接覆盖式输出到同级 `ui`）；产物名固定为 `assets/[name]-[hash].{js,css}`；dev 端口 5180 |
| `tsconfig.json` | target ES2022、module ESNext、`strict: true`、`noUnusedLocals/noUnusedParameters: true`、`noEmit: true`、`paths: {"@/*": ["src/*"]}` |
| `tailwind.config.js` | `darkMode: ['class', '[data-theme="dark"]']`；语义色板（primary/success/danger/warning/accent/download/neutral/page/surface/line/ink）全部映射到 CSS 变量 `rgb(var(--c-*) / <alpha-value>)` |
| `postcss.config.js` | 仅启用 tailwindcss 与 autoprefixer |
| `index.html` | 开发入口，引用 `/src/main.ts` |
| `tsconfig.tsbuildinfo` | 增量编译缓存残留（307 B） |
| `node_modules/` | 2748 个文件，为依赖安装结果 |
| `package-lock.json` | 73,261 B，依赖锁定 |

### 3.2 src 下 14 个文件（8 个模块 + 6 个页面）

| 文件 | 体积 / 行数 | 职责 |
|------|-------------|------|
| `main.ts` | 5321 B / 168 行 | 应用外壳与路由：定义 `PageId`（tasks/scanner/servers/profiles/history/settings 六页），渲染顶栏、品牌区与移动端菜单，`navigate()` 切换页面，`main()` 完成初始化 |
| `api.ts` | 5687 B / 146 行 | HTTP 客户端：`BASE = '/app/fvcc/api'`，统一 `request<T>()` 封装（含错误信息提取），导出 `api` 对象；另含基于 `EventSource` 的事件流 |
| `ws.ts` | 1318 B / 48 行 | WebSocket 客户端类：连接 `${proto}//${host}/app/fvcc/ws`，监听/取消监听、断线自动重连，导出单例 `ws` |
| `store.ts` | 2540 B / 101 行 | 全局状态单例：持有 `tasks / history / servers / profiles / metrics / appInfo`，订阅通知模式，并在收到 WS 消息时更新对应任务 |
| `types.ts` | 5621 B / 244 行 | 数据类型契约（文件头注释明确"与后端 models.go 对齐"）：`TaskStatus`（10 态）、`RetryType`、`Task`、`Server`、`Profile`、`StreamInfo`、`VideoInfo`、`Metrics`、`AppInfo`、`BrowseEntry`、`BrowseResult`、`Settings`，以及 `LOG_LEVELS`、`STATUS_LABEL`、`STATUS_CLASS`、`isTerminal()` |
| `ui.ts` | 9910 B / 181 行 | 轻量 UI 工具：`svgIcon()`（约 30 个内置图标路径）、`el()` 元素工厂、`formatSize/formatDuration/formatTime` 格式化、`toast()`、`confirmDialog()`、`emptyState()` |
| `theme.ts` | 2081 B / 62 行 | 主题管理：`themeOptions` 注册表、`getCurrentTheme/setTheme/initTheme`，`localStorage['fvcc-theme']` 持久化，兼容 `fnos-theme-mode` 与系统暗色偏好 |
| `style.css` | 6890 B / 255 行 | Tailwind 三层指令 + `:root` / `[data-theme="dark"]` 颜色变量定义（亮暗两套语义色） |

### 3.3 pages 下 6 个页面模块

| 页面模块 | 体积 / 行数 | 导出 | 职责 |
|----------|-------------|------|------|
| `tasks.ts` | 13,677 B / 420 行 | `renderTasks` | 任务列表页：多选集合、拖拽排序（`dragSrcId`）、状态标签与配色、时间格式化 |
| `scanner.ts` | 46,984 B / 1205 行 | `renderScanner`、`openDirBrowser` | 扫描页：媒体信息展示（码率/时长/体积）、目录浏览选择器（被 profiles 复用） |
| `servers.ts` | 9514 B / 247 行 | `renderServers` | 远程服务器管理页：列表、编辑弹窗、连通性测试 |
| `profiles.ts` | 64,260 B / 1268 行 | `renderProfiles` | 转码方案管理页，为整个前端**体量最大**的模块 |
| `history.ts` | 7377 B / 181 行 | `renderHistory` | 历史记录页：按 `fileName/status/serverId/createdAt/updatedAt` 多键排序 |
| `settings.ts` | 16,148 B / 385 行 | `renderSettings` | 设置页：日志级别等参数（`LOG_LEVELS`）、应用信息、主题切换 |

页面模块统一遵循同一范式：`import` 共享 `store / api / ui / types` → 导出单个 `renderXxx(container)` 函数 → 内部用 `el()` 构造 DOM。`scanner.ts` 的 `openDirBrowser` 被 `profiles.ts` 跨页复用，说明页面之间仅通过导出函数耦合，无全局隐式依赖。

---

## 四、关键技术结论

1. **生产资源前缀固定为 `/app/fvcc/`**：`vite.config.ts` 的 `base`、生产 `index.html` 中 JS/CSS 的引用路径、`api.ts` 的 `BASE = '/app/fvcc/api'`、`ws.ts` 的 `/app/fvcc/ws`、`ui/config` 的 `gatewayPrefix` 与 `url` 五处完全一致。任何一处改动都会导致资源 404 或接口不通。
2. **开发代理指向 127.0.0.1:8088**：dev 端口 5180，`/app/fvcc/api` 与 `/app/fvcc/ws`（`ws: true`）均代理到 `http://127.0.0.1:8088`，即 Go 后端本地监听端口。
3. **构建产物直接覆盖同级 `ui/`**：`outDir: '../ui'` 且 `emptyOutDir: false`，产物名 `assets/[name]-[hash].js|css`，因此 `ui/assets` 下两个带哈希文件名即是 `npm run build` 的输出。
4. **桌面入口与包清单通过字符串绑定**：`manifest.desktop_applaunchname = fvcc.Application` = `ui/config` 的 `.url` 键名；`manifest.desktop_uidir = ui` 指向界面目录；入口类型为 iframe，网关 socket 为 `app.sock`。
5. **运行时文件位置由生命周期脚本决定**：socket 放 `${TRIM_APPDEST}/app.sock`，日志与 PID 放 `${TRIM_PKGVAR}`，二进制通过 `FVCC_SOCK / FVCC_UIDIR / FVCC_DATADIR` 三个环境变量获取全部路径，因此二进制本身不硬编码目录。
6. **主题双通道**：页面首屏由 `index.html` 内联脚本读 `fnos-theme-mode` 决定 `data-theme`，运行时由 `theme.ts` 读写 `fvcc-theme`，Tailwind 以 `[data-theme="dark"]` 作为暗色选择器——三处需同时对齐。
7. **类型契约来自 `types.ts` 且声明与后端对齐**：文件头注释"与后端 models.go 对齐"，`TaskStatus` 共 10 个状态（QUEUE、UPLOADING、WAITING_TRANS、TRANSCODING、WAITING_DOWN、DOWNLOADING、COMPLETED、ERROR、PAUSED、CANCELLED），并配套 `STATUS_LABEL` / `STATUS_CLASS` / `isTerminal()`。

---

## 五、与自研 FVCC 项目的关系

自研项目位于 `D:\Fnos.VideoConversion\FVCC`，其 `server/` 为 Go 源码工程（`main.go / models.go / router.go / handlers.go / ws.go / scheduler.go / store.go / ffprobe.go / remote.go / security.go / gateway.go / localtranscode*.go / logger/ / smbshare/` 等），`app/ui` 为手写前端资源，另有 `cmd/`、`config/`、`manifest`。

### 5.1 可直接复用的点

| 复用点 | 官方包提供的内容 | 自研现状与差异 |
|--------|------------------|----------------|
| **接口契约比对基线** | `ui-src/src/types.ts`（244 行）：`Task/Server/Profile/Metrics/AppInfo/BrowseResult/Settings` 等类型与状态枚举、`STATUS_LABEL/STATUS_CLASS/isTerminal` 辅助 | 自研后端为 `server/models.go`（13,764 B），可用 `types.ts` 逐类型比对字段与状态取值是否一致，避免前后端契约漂移 |
| **前端实现替代仿制** | `ui-src/` 为**完整可编译源码**（含 `package.json` / `vite.config.ts` / `tailwind.config.js` / `tsconfig.json`），配合顶层 `fvcc` 二进制即可复现官方界面 | 自研 `app/ui` 为手写 JS/CSS（`assets/js`、`assets/css`，index.html 1276 B），可直接以 `ui-src` 为基线替换，省去仿制成本 |
| **打包校验基准** | 官方 `manifest`（478 B）、`cmd/` 9 脚本命名、`config/privilege` + `config/resource`、`ui/config` 入口声明 | 自研 `manifest`（396 B）缺 `platform` / `checksum` 两行，`maintainer`/`distributor` 为"墨迹"；`cmd/` 脚本名与 `main` 内容一致；`config/privilege`（64 B）、`config/resource`（572 B）同为同类声明；自研 `app/ui/config` 缺 `url` 字段、`title` 使用 `{display_name}` 占位、`allUsers` 为 `false`，与官方（`title: "FVCC"`、含 `url`、`allUsers: true`）存在差异 |
| **产物形态校验** | 官方 `ui/assets` 为 `index-<hash>.js` + `index-<hash>.css` 两个哈希命名文件 | 自研 `app/ui/assets` 仅有 `js`、`css` 两个子目录，与官方产物组织方式不同 |

### 5.2 使用建议

- 以 `ui-src/src/types.ts` 为唯一契约参照，逐条核对 `server/models.go` 的字段与枚举，再把差异同步到自研前端。
- 若采用官方 `ui-src` 作为新前端基线，注意其 `vite.config.ts` 的 `outDir: '../ui'` 是相对 `ui-src/` 输出的，需按自研目录结构重新指定输出位置。
- 打包自研 FPK 时，可用官方包作为结构模板逐项比对：顶层 12 项、`cmd` 9 脚本命名、`manifest` 关键四字段（`desktop_uidir`/`desktop_applaunchname` 与 `ui/config` 的 `.url` 键名一致）。

---

## 六、注意事项

1. **`ui/assets` 是压缩混淆产物**：`index-84EeMQNW.js`（102 KB）与 `index-e9ChqpP5.css`（23.5 KB）由 Vite 构建生成，不可读、不可直接修改；任何前端改动都应在 `ui-src/` 中进行后重新构建。
2. **`node_modules` 无需纳管**：`ui-src/node_modules` 占 2748 个文件，是解包后总体积（102.24 MB）的主要来源；剔除后整个包仅 46 个文件 / 44.02 MB。纳入版本管理或二次打包时应排除，仅保留 `package.json` 与 `package-lock.json`。
3. **空目录 `server/`、`wizard/`、`www/` 为保留位**：三者均为 0 文件，是官方包结构预留位置，不代表功能缺失，也不应据此推断存在未发布的隐藏内容。
4. **`.DS_Store` 为 macOS 残留**：`ui/.DS_Store`、`ui/images/.DS_Store` 各 6148 B，无功能意义，可与 `ICON.PNG` / `icon_256.png` 的命名大小写差异一并视为原始打包痕迹。
5. **`app.tgz` 与顶层目录同源**：顶层 `ui/`、`ui-src/` 来自 `app.tgz` 解包，该归档（23.6 MB）仍保留在顶层。二次分发时保留与否需自行决策，但重新打包应保持"外层 tar.gz + 内层 app.tgz"的层级。
6. **`fvcc` 二进制不可在 Windows 直接运行**：ELF 64-bit、x86-64、Linux 目标平台；Windows 下仅可静态分析，运行验证需在 FNOS / Linux 环境进行。
7. **`cmd/uninstall_init` 与 `cmd/uninstall_callback` 注释相同**：均写为 "called after the user uninstalls the application"，属官方原文，非解包错误。
8. **`tsconfig.tsbuildinfo` 为构建缓存**：307 B，无源码意义，不应作为配置依据。

---

*本文全部结论均可在 `D:\Fnos.VideoConversion\FVCC_official_unpack\` 下对应文件中逐项复核。*

