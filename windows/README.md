# Pulse for Windows

Pulse Windows 版是一个轻量、本地优先的 AI 编程额度监视器，使用原生 **Go + Win32 API + GDI+** 编写（无 Electron/Chromium 运行时开销，内存占用约 10–18 MB），支持屏幕边缘悬浮显示与系统托盘显示。

## 核心功能

- **屏幕边缘悬浮面板**：
  - 支持停靠在屏幕**右侧、左侧、顶部、底部**，或**自由竖向/横向悬浮**。
  - 支持拖动实时吸附屏幕边缘、跨多显示器拖动、可选**跟随鼠标所在显示器**。
  - 支持**闲置自动收起为边缘细线**（超限时细线自动变红提示），鼠标移上立即展开。
  - 鼠标悬停任意圆环即显示详情卡片：各时间窗口限额、连续/分段进度条、已用或剩余百分比、重置倒计时、消耗速度预测与余额。
  - 支持**深邃纯黑 / 浅色 / 半透明**材质，以及**截图/录屏时隐藏**（`WDA_EXCLUDEFROMCAPTURE`）。
- **系统托盘显示与快捷面板**：
  - 系统托盘实时绘制当前最高用量圆环（或 Pulse 标志）与悬浮提示。
  - 左键单击托盘图标打开/收起快捷面板弹窗；右键支持快速开关悬浮窗、立即刷新、开机自启、打开设置与退出。
  - 支持全局快捷键（默认 `Ctrl+Alt+P`）一键显示/隐藏悬浮窗。
- **安装版与独立版**：
  - 提供图形化安装向导 `PulseSetup.exe`（基于 Win32 + GDI+ 原生界面，内置完整应用图标）：支持自定义安装目录（默认 `%LOCALAPPDATA%\Programs\Pulse`）、创建桌面与开始菜单快捷方式、配置开机自启动，以及在 Windows「应用和功能」中一键卸载（`Pulse.exe --uninstall`）。
  - 也可直接独立运行免安装的 `Pulse.exe`。
- **内置主流 AI 服务商（22 个）**：
  - **本地自动读取**：Claude Code、Codex、Cursor（纯 Go 读取 `state.vscdb` + Windows DPAPI 解密）、Antigravity（自动探测本机 Language Server 端口与 CSRF Token）、Gemini CLI、Grok CLI。
  - **OAuth / Key 配置**：GitHub Copilot（内置 GitHub Device Code 登录）、DeepSeek、Kimi Code、z.ai、Zhipu（GLM Coding Plan）、MiniMax（支持 `minimax.io` / `minimaxi.com`）、OpenCode Go、Command Code、Volcengine（`arkcli`）、Alibaba Coding Plan、Moonshot、OpenAI API、Kilo Code、sub2api、New API、Kiro。
  - 所有手动输入的 API Key 均通过 **Windows DPAPI** 加密保存在 `%APPDATA%\Pulse\keys.dat`。
- **脚本支持**：
  - 支持 `Pulse.exe --json` 输出与 macOS 版兼容的 JSON 缓存快照。

## 从源码构建（Windows）

需要 Go 1.24+（本仓库在 Go 1.27 `windows/amd64` 下验证，无需 CGO / C 编译器）：

```powershell
cd windows
go test ./...
.\build.ps1
```

`.\build.ps1` 会自动完成：
1. 调用 `cmd/genrsrc` 从 `../AppIcon/pulse-icon-1024.png` 生成多分辨率 `.ico` 及 PE 资源文件（`*.syso`），为 `Pulse.exe` 与 `PulseSetup.exe` 嵌入应用图标与 Per-Monitor V2 DPI 清单；
2. 构建主程序 `Pulse.exe`；
3. 使用 `-tags installer` 将 `Pulse.exe` 压缩打包进图形安装程序 `PulseSetup.exe`。

构建完成后，直接运行 `.\PulseSetup.exe` 即可打开安装向导（或运行 `.\Pulse.exe` 绿色免安装启动）。
