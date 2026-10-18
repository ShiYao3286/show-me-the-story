# Windows 安装包构建说明

> 仓库：<https://github.com/ShiYao3286/show-me-the-story>
> 工作流：[windows-installer.yml](../../.github/workflows/windows-installer.yml)
> 产物：`ShowMeTheStory-Setup-<version>-windows-amd64.exe`（简体中文向导）

---

## 1. 怎么拿到安装包

### 方式一：手动触发（推荐用于测试）

```bash
gh workflow run windows-installer.yml --ref main -f version=v4.0.2-ui
gh run watch                      # 等它跑完
gh run download <run-id>          # 下载 artifact
```

或者在网页上：仓库 → **Actions** → **Windows Installer** → **Run workflow** → 填版本号 → Run。

### 方式二：打 tag 自动发布

```bash
git tag v4.1.0
git push origin v4.1.0
```

会依次跑 `check`（双平台构建+测试）→ `windows-installer`（产出安装包）→ `release`（把安装包和 5 个平台的压缩包一起挂到 GitHub Release）。

### 本地构建

```bash
# 前置：Go 1.25.1、Node 24、NSIS 3.x（makensis 在 PATH）、Python + Pillow
cd frontend && npm ci && npm run build && cd ..
python tools/make_icon.py                    # 从 logo 生成 7 个尺寸的图标
go run ./tools/buildwin -version v4.0.2 -out dist
# 产物：dist/show-me-the-story.exe 和 dist/ShowMeTheStory-Setup-*.exe
```

---

## 2. 安装包做了什么

| 项 | 实现 |
|---|---|
| **向导语言** | 简体中文（`SimpChinese`），英文兜底 |
| **安装范围** | 每用户安装（`%LOCALAPPDATA%\Programs\ShowMeTheStory`），**不需要管理员权限** |
| **快捷方式** | 开始菜单 + 桌面（桌面为可选组件） |
| **卸载入口** | 控制面板「应用和功能」中注册，含图标、版本、发布者 |
| **数据保护** | 卸载时**默认询问并保留** `storys/`、`api.json`、`skills/`；用户主动选择才删除 |
| **升级** | 检测到已安装版本时提示，升级不覆盖小说数据 |
| **数据目录** | 固定为安装目录，避免双击时落到 `System32` |

### 向导页面

1. **欢迎页** — 说明这是本地工具、会打开浏览器、数据存在哪里、卸载不删数据
2. **安装目录** — 可改，默认 `%LOCALAPPDATA%\Programs\ShowMeTheStory`
3. **安装进度**
4. **完成页** — 勾选「立即启动 AI 小说写手」

---

## 3. exe 本身的改动

| 改动 | 效果 | 怎么做的 |
|---|---|---|
| **PE Subsystem: CUI → GUI** | 双击**不再弹黑色控制台窗口** | `-ldflags "-H windowsgui"` |
| **嵌入图标** | 资源管理器/任务栏/快捷方式显示项目 logo | `go-winres` 生成 `.syso`，7 个尺寸（16–256） |
| **版本信息** | 文件属性「详细信息」显示产品名、版本、公司、MIT 许可 | 同上，`RT_VERSION` 资源 |
| **应用清单** | 高 DPI（per-monitor v2）、长路径、`asInvoker` 权限 | `RT_MANIFEST` + `app.manifest` |

体积：**9.24 MiB → 9.40 MiB**（+160 KB，主要是图标资源）。

---

## 4. 文件结构

```
build/windows/
├── app.manifest       Windows 应用清单（DPI/长路径/权限）
├── winres.json        go-winres 资源定义（图标 + 版本信息 + 清单）
├── installer.nsi      NSIS 中文向导脚本
├── icon.ico           ← CI 生成，不提交
└── icon_*.png         ← CI 生成，不提交
tools/
├── buildwin/main.go   纯 Go 构建驱动：图标 → syso → GUI exe → 安装包
└── make_icon.py       从 docs/show-me-the-story.webp 生成图标集
.github/workflows/
└── windows-installer.yml   Windows runner 上构建安装包
```

**为什么图标不提交**：PNG/ICO 是生成物，且会让仓库多 200 KB 二进制。CI 每次用 `tools/make_icon.py` 从原始 logo 重新生成，保证和 logo 一致。想本地构建时需要先跑那个脚本（`buildwin` 会检查并在缺失时给出明确提示）。

---

## 5. CI 流程

```yaml
windows-latest
  1. checkout
  2. setup-node 24 + npm cache
  3. setup-go（用 go.mod 的 1.25.1）
  4. 解析版本号（tag / 手动输入 / dev）
  5. choco install nsis        # 装 makensis
  6. pip install pillow        # 生成图标用
  7. npm ci && npm run build   # 前端（embed 进去）
  8. python tools/make_icon.py # 图标
  9. go run ./tools/buildwin   # exe + 安装包
 10. 冒烟测试：启动 exe，轮询 /api/version，然后杀掉
 11. 上传 artifact（安装包 + exe）
```

**冒烟测试**是特意加的——因为 GUI 子系统程序没有控制台，出错时很容易静默失败。CI 里用独立端口（48099）启动、轮询 API、再 `Stop-Process`，能抓到「构建成功但跑不起来」的情况。

---

## 6. 已验证与未验证

### 已在本机验证 ✅

| 项 | 结果 |
|---|---|
| 云端构建 | run [39098868555](https://github.com/ShiYao3286/show-me-the-story/actions/runs/39098868555) **success**，17 个步骤全绿 |
| 产物 PE 属性 | exe 与安装包均为 Subsystem=2 (GUI)、含 `.rsrc` 资源段 |
| 版本信息 | `FileDescription=Show Me The Story - AI novel writing workspace`、`FileVersion=4.0.2-ui`、`CompanyName=xianii` |
| 中文资源 | 安装包内含 UTF-16 中文串（`AI 小说写手` 等）+ **LCID 2052**（简体中文） |
| GUI 运行 | 本机启动 exe，`/api/version` 返回 `v4.0.2-ui`，无控制台窗口 |
| 前端 | 云端构建的 exe 内嵌的是带纸感主题的新前端 |
| 回归 | `go build/vet/test` 全过；`npm run check` 4/4 通过 |

### 未能验证 ⚠️

**安装包的实际安装行为**（向导界面、文件落地、注册表、快捷方式、卸载）在本机无法验证——DSH 沙箱会直接终止 NSIS 安装器进程（连一个只写一个文本文件的最小安装器也跑不起来，返回 exit 0 但零落地）。这是环境限制，不是脚本问题。

**建议你在自己的机器上跑一次交互式安装来确认**：
```powershell
.\ShowMeTheStory-Setup-4.0.2-ui-windows-amd64.exe
```
重点看：向导是否中文、装完能否从开始菜单启动、控制面板里能否卸载、卸载时是否询问保留数据。

---

## 7. 已知限制

| 限制 | 说明 | 后续可做 |
|---|---|---|
| **未代码签名** | Windows SmartScreen 会提示「未知发布者」，需点「更多信息 → 仍要运行」 | 买 OV/EV 代码签名证书（约 $100–400/年），在 CI 里用 `signtool` 签名 |
| **未内嵌 WebView2** | 仍是「启动后用默认浏览器打开」的形态，不是原生窗口 | 见下面的演进建议 |
| **中文安装路径** | 安装目录建议用默认（纯英文路径），程序本身支持中文路径但部分第三方杀软可能误报 | — |

---

## 8. 如果想继续演进

| 优先级 | 项 | 说明 |
|---|---|---|
| 高 | **代码签名** | 消除 SmartScreen 警告，这是分发体验的最大短板 |
| 中 | **WebView2 原生窗口** | 用 `webview/webview_go` 包一层，去掉浏览器地址栏；体积 +2–4 MiB，但需要引入 Go 依赖（破坏「零依赖」） |
| 中 | **安全默认值** | 默认绑 `127.0.0.1` 而非 `0.0.0.0`；`GET /api/config/api` 掩码 api_key。**打包成"软件"后用户会更信任它，这两点应该优先修** |
| 低 | **自动更新** | 现在有 GitHub 更新检查（只显示徽章），可做成「下载并静默替换」 |
| 低 | **多语言安装包** | 目前中文为主、英文兜底；可加繁中/日文 |
