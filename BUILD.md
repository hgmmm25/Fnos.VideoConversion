# FVCC / FVCS 构建规范

> 最后更新: 2026-09-19  
> 根目录: `D:\Fnos.VideoConversion`

## 目录

| 文件 | 用途 |
|------|------|
| [build.ps1](./build.ps1) | 一键构建脚本（推荐） |
| [build-env.ps1](./build-env.ps1) | 构建环境配置（source 后使用） |

## 快速开始

```powershell
cd D:\Fnos.VideoConversion
.\build.ps1                     # FVCC + FVCS 全量构建
.\build.ps1 -Target FVCS        # 仅 FVCS
.\build.ps1 -Target FVCC        # 仅 FVCC
.\build.ps1 -Clean -Target FVCS # 清理后仅构建 FVCS
.\build.ps1 -NoTest -Target FVCS # 不跑测试直接构建
```

## 架构

```
D:\Fnos.VideoConversion\
├── FVCC/                              # NAS 调度端
│   ├── server/                        # Go 后端 (gin, 无 CGO)
│   ├── ui-src/                        # 前端 (Vite + TypeScript)
│   ├── cmd/                           # fnOS 安装脚本
│   ├── manifest                       # fnOS 应用清单
│   ├── app/                           # 构建产物目录 (fvcc + ui/)
│   ├── ui-src/public/                 # vite 静态资源源 (config + images，构建时自动复制回 app/ui)
│   └── temp/fpk_stage/                # fnpack 打包用的干净 stage 目录（每次构建自动重建）
├── fnpack-1.2.3-windows-amd64.exe     # fnOS 打包工具 (1.2.3，全仓库统一使用)
├── archive/                           # 仓库治理归档（P2-3）：历史版本 fpk / 旧工具 / 升级前备份
│   ├── fpk/                           # 历史版本包 FVCC_v1.1.0 ~ v1.5.3
│   ├── tools/                         # 旧版 fnpack-1.2.1.exe（仅留存）
│   └── _backup_* / *.bak              # 升级前备份与临时文件
├── FVCS/                              # Windows 渲染端
│   ├── cmd/service/main.go            # 服务入口
│   ├── cmd/settings/                  # fyne 桌面 UI (需 CGO)
│   ├── cmd/ui/                        # fyne 桌面 UI (需 CGO)
│   └── pkg/                           # 业务逻辑
└── build.ps1                          # 一键构建脚本
```

## FVCC 构建流程（最新，2026-09-17）

```
前端 (Vite)     → npm run build          → FVCC\app\ui\   (含 config + images，来自 ui-src/public)
后端 (Go)       → go build Linux ELF     → FVCC\app\fvcc
【前后端并行执行】
fnOS 打包       → fnpack-1.2.3 build     → 在干净 stage 目录 FVCC\temp\fpk_stage\ 生成 fvcc.fpk
产物回收        → Copy-Item              → FVCC\fvcc.fpk（覆盖）
```

推荐直接使用一键脚本（已内嵌全部步骤）：

```powershell
cd D:\Fnos.VideoConversion
.\build.ps1 -Target FVCC        # 仅构建 FVCC 并产出 fvcc.fpk
.\build.ps1                     # FVCC + FVCS 全量构建（ALL）
.\build.ps1 -Target FVCS        # 仅 FVCS（不产出 fpk）
```

### FVCC 构建前置条件（依赖）

| 工具 | 版本 | 路径 |
|------|------|------|
| Go (FVCC 后端) | go.mod 声明 `go 1.27.1` + `GOTOOLCHAIN=auto`（本机 PATH 上 Go 实测 1.27.1，模块缓存已含 `golang.org/toolchain@v0.0.1-go1.27.1`） | 启动器 `C:\Users\xx318\sdk\go1.25.3\go\bin\go.exe` 或 `C:\Program Files\Go\bin\go.exe`（build.ps1 按此顺序探测） |
| Node.js | v26.11.1（系统级 MSI，2026-10-13 由 v25.9.0 升级；CI 同为 26，`@types/node` 已抬至 ^26，`package.json` 声明 `engines.node >= 26.0.0`） | `C:\Program Files\nodejs\node.exe`（系统 PATH 首位） |
| npm | 内置 | 系统 PATH |
| fnpack | 1.2.3（打包工具） | `D:\Fnos.VideoConversion\fnpack-1.2.3-windows-amd64.exe` |

### FVCC 版本号同步清单（升级版本时必须三处一致）

| 文件 | 当前值 |
|------|--------|
| `FVCC\manifest`（`version` 字段） | 1.6.8 |
| `FVCC\ui-src\package.json`（`version` 字段） | 1.6.8 |
| `FVCC\server\internal\version\VERSION` | 1.6.8 |

> 版本号不一致会导致 fpk 内 manifest 声明与实际产品版本不符，部署后难以排查。
> **三处缺一不可**：`VERSION` 是 `go:embed` 进二进制的真源，决定 `/api/info` 与「设置」页显示值；历史缺陷 v1.6.4 只改了 manifest + package.json，导致已推送包显示 1.6.0。
> 收口必跑 `scripts\check-versions.ps1`（仓库根按脚本位置推导，可用 `-RepoRoot` 覆盖）；在 `FVCC\server` 下 `go test ./internal/version` 现已在 Windows 本机真跑并在漂移时判红（不再静默 SKIP）。
> 另：`ui-src\package-lock.json` 的根 `version` 字段随 `package.json` 手工 bump 时**不会自动跟进**（校验脚本也不覆盖它，v1.6.8 实测漂留在 1.6.7），改完 `package.json` 版本后跑一次 `npm install --package-lock-only`（只刷锁文件元数据，不动 `node_modules`）即可同步。

### FVCS 版本号同步清单（两处，第二处最容易漏）

| 文件 | 当前值 |
|------|--------|
| `FVCS\pkg\version\VERSION`（`go:embed` 真源） | 1.3.1 |
| `FVCS\pkg\version\version_test.go`（对齐断言里的字面量） | 1.3.1 |

> FVCS 的校验用例把期望值写成硬编码字面量，只改 `VERSION` 会让 `go test ./pkg/version` 判红；v1.3.1 批次即按两处同步。FVCS 不在 `check-versions.ps1` 的三真源比对范围内（该脚本只校 FVCC）。

### FVCC 构建步骤明细（对应 build.ps1 内部实现）

1. **前端**：在 `FVCC\ui-src` 执行 `npm run build`（内部为 `check:design:diff && tsc -b && vite build`），产物输出到 `FVCC\app\ui`。`vite.config.ts` 中 `emptyOutDir: true` 会清空 `app\ui`，但 `ui-src\public\config` 与 `ui-src\public\images\` 会被 vite 自动复制回 `app\ui`，保证 fnOS 必需资源不丢失。
2. **后端**：在 `FVCC\server` 模块目录内执行：
   ```powershell
   go build -trimpath -ldflags "-s -w" -o ../app/fvcc .
   ```
   - 必须在 server 模块目录内（`Push-Location`），否则报 `cannot find main module`；
   - `GOTOOLCHAIN=auto`（不要用 `local`，go.mod 要求 go 1.27.1，auto 会自动匹配缓存工具链）；
   - `-trimpath -ldflags "-s -w"` 瘦身减小 fpk 体积。
3. **并行**：前端 npm 与后端 go build 通过 `Start-Job` 并行执行（实测各 1.3~2.3s，并行后总时长由两者之和降为 max 值）。
4. **打包（fnpack 1.2.3）**：在干净 stage 目录 `FVCC\temp\fpk_stage` 执行（目录每次自动重建，仅含 `app/ cmd/ config/ manifest ICON.PNG ICON_256.PNG fnpack.exe`）：
   ```powershell
   .\fnpack.exe build
   ```
   - 必须用干净目录：fnpack 验证阶段会**递归扫描工作目录**，真实 FVCC 目录含 temp/logs/历史 fpk 等数千文件时打包耗时 ~14s，干净目录仅 ~2s；
   - 成功后输出文本 `Packing successfully. The output file fvcc.fpk can be found in the working directory.`
   - **成功判定必须以输出文本为准**：fnpack 失败（如 `Packing failed. Required file ... is missing`）时 exit code 仍为 0，只看退出码会误报成功（build.ps1 已内置文本校验）。
5. **产物回收**：将 `fpk_stage\fvcc.fpk` 复制回 `FVCC\fvcc.fpk` 覆盖。

### FVCC 环境变量（build.ps1 已内建，手动构建时参考）

```powershell
$env:CGO_ENABLED      = "0"
$env:GOOS             = "linux"
$env:GOARCH           = "amd64"
$env:GOTOOLCHAIN      = "auto"
$env:GOCACHE          = "D:\GoCache"
```

### fpk 产物校验方法（构建后建议执行）

```python
# Python 3，解包校验关键项
import tarfile, hashlib, io
tf = tarfile.open(r"D:\Fnos.VideoConversion\FVCC\fvcc.fpk")
print(tf.getnames())                      # 顶层应含 app.tgz / cmd / config / ICON.PNG / ICON_256.PNG / manifest
apptgz = tf.extractfile("app.tgz").read()
manifest = tf.extractfile("manifest").read().decode("utf-8")
md5 = hashlib.md5(apptgz).hexdigest()
assert md5 in manifest, "manifest checksum 与 app.tgz 不一致"   # checksum = md5(app.tgz)
at = tarfile.open(fileobj=io.BytesIO(apptgz))
ui = [m.name for m in at.getmembers()]
assert "ui/config" in ui and "ui/images/icon_64.png" in ui and "ui/images/icon_256.png" in ui
ic64  = tf.extractfile("ICON.PNG").read()
ic256 = tf.extractfile("ICON_256.PNG").read()
assert ic64  == at.extractfile("ui/images/icon_64.png").read()
assert ic256 == at.extractfile("ui/images/icon_256.png").read()
print("OK: 成员完整 / checksum 一致 / 图标一致")
```

校验要点：① 顶层成员完整；② `manifest.checksum == md5(app.tgz)`；③ `app.tgz` 内含 `ui/config`、`ui/images/icon_64.png`、`ui/images/icon_256.png`；④ 顶层 `ICON.PNG`/`ICON_256.PNG` 与 `ui/images/` 内图标字节一致。

## FVCS 构建流程

```
编译 (Go, CGO) → go build Windows →  D:\Fnos.VideoConversion\FVCS\fvcs-service.exe
```

### FVCS 环境要求

| 工具 | 版本 | 路径 |
|------|------|------|
| Go | go.mod 声明 `go 1.27.1`（与 FVCC 同一口径，`GOTOOLCHAIN=auto` 选取） | 本机 PATH 上的 Go 1.27.1 直接可用；历史「FVCS 用 go1.24.6 + `GOTOOLCHAIN=local`」的写法已作废 |
| GCC (MSYS2) | mingw-w64 | `C:\msys64\mingw64\bin\gcc.exe` |

### FVCS 环境变量

```powershell
$env:GOTOOLCHAIN  = "auto"   # 双端统一；local 会把版本锁死在启动器自身
$env:CGO_ENABLED  = "1"
$env:CC           = "C:\msys64\mingw64\bin\gcc.exe"   # 必须显式指定，cgo 才能找到编译器
$env:PATH         = "C:\msys64\mingw64\bin;$env:PATH"
```

> **重要**：不要设置 `CGO_CFLAGS` / `CGO_LDFLAGS`。实测设置了 `CGO_LDFLAGS=-L...\lib` 会导致 `runtime/cgo` 编译直接失败（exit status 2）；Go 会自行找到 mingw64 的 include/lib。

## 注意事项

### 测试范围

FVCS 测试必须只包含 `./pkg/...` 和 `./cmd/service`：

```powershell
go test ./pkg/... ./cmd/service -count=1 -v
```

不要使用 `go test ./...`，它会包含 `cmd/ui` 和 `cmd/settings`，这两个包需要 fyne GUI 依赖（CGO），冷编译耗时 360+ 秒。

### Go 工具链

- **双端统一 `GOTOOLCHAIN=auto`**：`FVCC/server/go.mod` 与 `FVCS/go.mod` 都声明 `go 1.27.1`，auto 按 go.mod 选取工具链（模块缓存已含 `golang.org/toolchain@v0.0.1-go1.27.1`，无额外下载）。`build.ps1` 探测启动器的顺序是 `sdk\go1.25.3` → `Program Files\Go` → PATH 上的 `go`，无论命中哪个，auto 都会升到 go.mod 要求的版本；旧的「FVCS 用 go1.24.6 + local」写法已作废（本机 `sdk\go1.24.6` 目录也已不存在）。
- **`go` 指令是下限，不是 pin**：上游已发布 go1.27.2（补丁版，另有 1.26.9），但把两个 go.mod 的 `go` 指令提到 1.27.2 只会让新环境必须下载该工具链，收益是补丁修复而非语言特性，故本仓库保持 `go 1.27.1` 下限、由 auto 在实际运行时选取本机/缓存里满足下限的工具链。
- **不要把 `GOCACHE` 留在 C 盘**。当前 C 盘剩余约 15 GB，GOCACHE 默认 2.5 GB 就在那里。
  - 推荐：在 `build-env.ps1` 中已配置 `GOCACHE` 到 `D:\GoCache`。
  - 手动设置：`$env:GOCACHE = "D:\GoCache"`

### 工具链与依赖版本基线（2026-10-13 审计复核，2026-10-14 v1.6.8 Tailwind 4 专项后复跑）

| 项 | 本仓库声明/锁定值 | 上游最新 | 处置 |
|----|----|----|----|
| Go 工具链 | 双端 go.mod `go 1.27.1`（语言下限，无 `toolchain` 行） | go1.27.2（1.27 线补丁） | **保持 1.27.1 下限**：`go` 指令不是 pin，补丁级改进由 auto + 本机工具链自然获得，抬高下限只会让新环境必装指定版本 |
| CI Go | `.github/workflows/fvcc-ci.yml` → `actions/setup-go@v5` + `1.27.x` | — | 与 go.mod 一致，无需改 |
| Node 运行时 | 本机 v26.11.1（系统级 MSI，`C:\Program Files\nodejs`；`AppData\Local\hermes\node` 有一份残留副本，PATH 排在系统目录之后故不生效） | 26.11.1（Current 线） | **已升级**（2026-10-13，从 v25.9.0——v25 是已 EOL 的奇数 Current 线）。注意 26 仍是 Current 而非 LTS（active LTS 为 24 Krypton），下次抬 LTS 时同步 CI 与 `engines` |
| CI Node | `.github/workflows/fvcc-ci.yml` → `actions/setup-node@v4` + `node-version: '26'` | 26.x | **与本机运行时、`engines.node`、`@types/node` 四处对齐** |
| FVCC 前端 | vite 8.3.4、TypeScript 7.0.2、postcss 8.5.30、tailwindcss 4.3.3 + `@tailwindcss/postcss` 4.3.3、lightweight-charts 5.2.1、@fontsource 5.3.0 | 全部当前 | 已随 v1.6.6 刷新；2026-10-14（v1.6.8）Tailwind 4 专项落地后 `npm outdated` 归零。**`autoprefixer` 已随 v4 移除**，浏览器前缀由 v4 内建 Lightning CSS 负责 |
| `@types/node` | ^26（锁 26.6.5） | 26.6.5 | **已随 Node 26 运行时同批刷新**（2026-10-13） |
| `tailwindcss` | ^4.3.3（锁 4.3.3） | 4.3.3 | **已升级**（2026-10-14 专项，v3.4.19→v4.3.3）：CSS-first 配置重写，`tailwind.config.js` 删除、token 映射迁入 `src/style.css` 的 `@theme inline`；同时清掉 tailwind 3 传递链带来的 7 项 `npm audit` 告警（2 moderate / 5 high）。细节见下节「Tailwind 4 样式链接线」 |
| FVCC Go 依赖 | gin v1.12.0、gorilla/websocket v1.5.3 | 均为当前 | 无落后项 |
| `modernc.org/sqlite` | v1.60.1（go.mod 直接依赖；驱动自身要求 `go 1.26.0`，仍低于本仓库 1.27.1 下限，无需抬） | v1.60.1 | **已升级**（2026-10-13 专项批次，v1.14.0→v1.60.1 跨 46 个 minor）：老驱动生成的 fvcc.db 由新驱动读取/迁移/回写实测通过，WAL、`schema_version`、AUTOINCREMENT 续号均保持；副作用是依赖面收窄（删 cc/v3、ccgo/v3、x/mod、x/tools 等 10 项，go.sum −109 行），代价是二进制 +565 KB（fpk +2.4%） |
| FVCS Go 依赖 | fyne v2.8.2、`fyne.io/systray` 伪版本 v1.12.3-0.20260810…、mattn/go-sqlite3 v1.14.52、golang.org/x/sys v0.49.0 | 均为当前 | systray 伪版本是 fyne 2.8.x go.mod 的 MVS 结果（覆盖不掉），本批已确认可接受 |

> 刷新纪律：补丁/次版本级"落后即可顺手升"，大版本与跨多 minor 的项一律另立专项；同主题多批次做完再统一 bump 版本、写台账。

### Tailwind 4 样式链接线（v1.6.8 起）

v3 → v4 是构建链层面的改动，手动构建或排障时需要注意四点：

- **PostCSS 插件换包**：`postcss.config.js` 只挂 `@tailwindcss/postcss`（v4 把 PostCSS 插件拆成独立包），不再写 `tailwindcss` 与 `autoprefixer` 两个插件名。
- **配置载体从 JS 迁到 CSS**：仓库内**已无 `tailwind.config.js`**。token（`--color-*`）、暗色变体（`@custom-variant dark`）、扫描面（`@import "tailwindcss" source(none)` + `@source`）全部声明在 `FVCC/ui-src/src/style.css` 头部；`--c-*` 是完整 `rgb()` 颜色而非裸 RGB 通道，`/NN` 透明度由编译期 `color-mix()` 生成。任何"沿用 v3 写法"（`<alpha-value>`、`rgb(var(--c-*))`）会被 `npm run check:design` 的 `TW4-token-form` 规则拦下。
- **扫描面必须显式锁定**：v4 默认的自动源探测会读入 `scripts/*.mjs`、`vite.config.ts` 等配置文件里的类名字样并编进产物（实测多出一个 `.shadow-sm`），所以用 `source(none)` 关掉自动、只喂 `index.html` 与 `src/**/*.{ts,js,html}`。新增源码目录时记得同步 `@source`。
- **构建入口不变**：`npm run build` 仍是 `check:design:diff` → `tsc -b` → `vite build`；CI（`.github/workflows/fvcc-ci.yml`）在 `FVCC/ui-src` 里 `npm ci` + build，无需改动。产物体积：CSS 原始 49,956 B（v3 为 42,782 B），**gzip 9.78 kB（v3 为 10.00 kB，反而变小）**。

### MSYS2 MinGW-w64

FVCS 的 CGO 构建依赖 MSYS2 MinGW-w64 的 gcc。确保 `C:\msys64\mingw64\bin` 在 PATH 中，且 `gcc.exe` 存在。

### Vite 清理

`vite.config.ts` 中 `emptyOutDir` 已设为 `true`。构建时会自动清理 `app/ui` 下的旧产物，无需手动删除。

> ⚠️ **坑位**：`emptyOutDir` 会同时清空 `app/ui/config`、`app/ui/images/`（fnOS 必需资源）。当前 `ui-src/public/config` 与 `ui-src/public/images/` 中的资源由 vite 自动复制回 `app/ui`，因此**禁止删除 ui-src/public 下的 config/images**；若改动后打包发现 `ui/config` 缺失，先检查 public 目录是否被误清。

### fnOS 部署

- FVCC 的 `fnpack` 需要 `app/fvcc`（Linux ELF）和 `app/ui/` 目录同时存在。
- 当前版本 `1.6.8`（配套渲染节点 FVCS `1.3.1`），在 manifest、package.json、server/internal/version/VERSION 中均已同步（升级版本必须三处同步，见上表）。
- fnpack 工具：`D:\Fnos.VideoConversion\fnpack-1.2.3-windows-amd64.exe`（1.2.3 可无缝替换 1.2.1，用法一致；旧版已归档至 `archive\tools\fnpack-1.2.1.exe` 仅留存）。
- 版本一致性校验：发布前运行 `.\scripts\check-versions.ps1`（P0-1 新增），校验 manifest / package.json / server/VERSION 三方对齐及 go.mod 工具链声明。
- 打包必须在干净 stage 目录 `FVCC\temp\fpk_stage` 内执行（build.ps1 自动完成），避免 fnpack 递归扫描真实目录导致耗时 ~14s。

## 故障排查

| 问题 | 原因 | 解决 |
|------|------|------|
| `runtime/cgo` exit status 2 | cgo 找不到 gcc（PATH 未含 mingw64 或未显式设 CC）；或设置了 `CGO_LDFLAGS` | PATH 加入 `C:\msys64\mingw64\bin` 并设 `CC=C:\msys64\mingw64\bin\gcc.exe`；删除 `CGO_CFLAGS/CGO_LDFLAGS` |
| `go-sqlite3` 编译失败 | CGO 未启用或 GCC 不在 PATH | 运行 `build-env.ps1` 设置环境变量 |
| `go-gl/gl` 编译失败 | `go test ./...` 包含 `cmd/ui` | 只测 `./pkg/... ./cmd/service` |
| `build constraints exclude all Go files` | 纯 Go 构建 CGO 依赖 | FVCS 必须 `CGO_ENABLED=1` |
| `internal/stringslite` 缺失 | std 源码不完整（多见于手工解压/被裁剪的 Go 安装） | 换用完整安装的 Go（PATH 上的 `C:\Program Files\Go`），由 `GOTOOLCHAIN=auto` 按 go.mod 选取；不要靠 `local` 绕 |
| FVCC 构建后无界面 | 缺少 `app/ui/` 或 `app/fvcc` | 先构建前端，再构建后端 |
| fnpack 打包耗时 ~14s | fnpack 验证阶段递归扫描工作目录（真实 FVCC 目录含 temp/logs/历史 fpk 等） | 在干净 stage 目录 `FVCC\temp\fpk_stage` 打包（build.ps1 已自动处理），耗时降至 ~2s |
| fnpack 报 `Packing failed. Required file ... is missing` 但脚本“成功” | fnpack 失败时 exit code 仍为 0 | 成功判定必须以输出文本 `Packing successfully` 为准，不要只查 `$LASTEXITCODE`（build.ps1 已内置校验） |
| `cannot find main module`（go build 报错） | 未在 server 模块目录内执行 | 进入 `FVCC\server` 后执行 `go build -trimpath -ldflags "-s -w" -o ../app/fvcc .` |
| `go.mod requires go >= 1.27.1` | 启动器 Go 版本低于 go.mod 声明，且 `GOTOOLCHAIN=local` 禁止了自动选取 | 改用 `GOTOOLCHAIN=auto`（`build-env.ps1` / `build.ps1` 均已统一 auto，双端同口径） |
| fpk 内 `ui/config` / 图标缺失 | vite `emptyOutDir` 清空 `app/ui` 且 ui-src/public 被误删 | 恢复 `ui-src/public/config`、`ui-src/public/images`，重新 `npm run build` |
