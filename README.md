<p align="center">
  <img src="AppIcon/pulse-icon-1024.png" width="112" alt="Pulse">
</p>

<h1 align="center">Pulse for Windows</h1>

<p align="center">
  <b>Know how much Claude Code, Codex, Cursor and Antigravity quota you have left — without opening a single usage page.</b><br>
  A free, open-source native Windows monitor written in Go + Win32 + GDI+ that docks cleanly along your screen edge and system tray.
</p>

<p align="center">
  <a href="https://github.com/feicai922/Pulse-win/releases/latest"><img src="https://img.shields.io/badge/Download-PulseSetup.exe-0078D4?style=for-the-badge&logo=windows&logoColor=white" alt="Download Pulse for Windows"></a>
</p>

<p align="center">
  <a href="https://github.com/feicai922/Pulse-win/releases/latest"><img src="https://img.shields.io/github/v/release/feicai922/Pulse-win?color=black" alt="Latest release"></a>
  <a href="https://github.com/feicai922/Pulse-win/releases"><img src="https://img.shields.io/github/downloads/feicai922/Pulse-win/total?color=black" alt="Downloads"></a>
  <a href="https://github.com/feicai922/Pulse-win/stargazers"><img src="https://img.shields.io/github/stars/feicai922/Pulse-win?color=black" alt="GitHub stars"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue" alt="Apache 2.0 license"></a>
</p>

<p align="center">
  <sub><b>Windows 10 / 11 (x64)</b> · Pure Go + Win32 + GDI+ (No Electron) · <b>English</b> · <a href="README.zh-CN.md"><b>简体中文</b></a> · <a href="windows/README.md"><b>Windows Dev Guide</b></a></sub>
</p>

---

- **Native & ultra-lightweight** — Built with pure Go (`CGO_ENABLED=0`), Win32 APIs, and GDI+ instead of Electron or WebView runtimes (~10–18 MB RAM, crisp Per-Monitor V2 DPI scaling).
- **22 AI coding providers** — Claude Code, Codex, Cursor, GitHub Copilot, Antigravity, Gemini CLI, Grok CLI, DeepSeek, Kimi Code, z.ai, Zhipu GLM Coding Plan, MiniMax, OpenCode Go, Kiro, and more.
- **No Pulse account, no telemetry** — Reads local credentials or connects directly to each provider using your own logins and API keys, encrypted locally with **Windows DPAPI**.
- **True Pulse visual design** — Edge-docked rail with concave berth flares, idle sliver auto-collapse, curved-tail detail cards, system tray live ring + dashboard popup, and a dark native Settings window.

---

## Installation

### Option 1: Windows Installer (Recommended)
1. Download **`PulseSetup.exe`** from [Releases](https://github.com/feicai922/Pulse-win/releases/latest).
2. Double-click `PulseSetup.exe`, choose your install folder (defaults to `%LOCALAPPDATA%\Programs\Pulse`), and pick optional Desktop/Start Menu shortcuts and launch-at-startup.
3. Uninstall cleanly anytime from **Windows Settings → Apps → Installed apps** (or `Pulse.exe --uninstall`).

### Option 2: Portable Executable
Download **`Pulse.exe`** from [Releases](https://github.com/feicai922/Pulse-win/releases/latest) and run it directly — no installation required.

---

## Key Features

### Floating Edge Panel
- **Dock anywhere** — Snap smoothly to the **right, left, top, or bottom** screen edge, or switch to free vertical/horizontal floating placement across multi-monitor setups (with optional **Follow Active Display**).
- **Auto-collapse sliver** — Folds into a thin indicator strip at the screen edge when idle (turns red when exhausted) and expands immediately on hover.
- **Hover detail cards** — Shows every usage window, continuous or segmented bars, used/remaining percentage, reset countdown, burn-rate exhaustion forecast, and account balance.
- **Surfaces & privacy** — Obsidian, Light, and Translucent themes plus **Hide from Screen Capture** (`WDA_EXCLUDEFROMCAPTURE`).

### System Tray & Shortcuts
- **Live tray usage ring** — Renders the highest usage ring (or Pulse mark) directly in the Windows notification area.
- **Left-click dashboard** — Left-click the tray icon to toggle a compact usage dashboard popup; right-click for quick panel toggle, manual refresh, startup toggle, Settings, and Quit.
- **Global hotkey** — Press `Ctrl+Alt+P` (customizable) to show or hide the floating panel.

### Supported Providers (22 Built-in)
- **Local auto-detection**: Claude Code, Codex, Cursor (pure-Go SQLite B-tree/WAL reader + Windows DPAPI Electron decryption), Antigravity (local Language Server PEB/loopback port discovery + `agy.exe` CLI fallback), Gemini CLI, Grok CLI.
- **OAuth / API Key**: GitHub Copilot (RFC 8628 Device Flow), DeepSeek, Kimi Code, z.ai, Zhipu (GLM Coding Plan), MiniMax (`minimax.io` / `minimaxi.com`), OpenCode Go, Command Code, Volcengine (`arkcli`), Alibaba Coding Plan, Moonshot, OpenAI Platform, Kilo Code, sub2api, New API, Kiro.

### Privacy & CLI
- Credentials are encrypted with **Windows DPAPI** in `%APPDATA%\Pulse\keys.dat`; settings live in `%APPDATA%\Pulse\settings.json`.
- Run `Pulse.exe --json` to print the cached usage report as JSON for scripts and status bars.

---

## Build from Source (Windows)

Requires **Go 1.24+** (verified on Go 1.27 `windows/amd64`; no C compiler or CGO needed):

```powershell
git clone https://github.com/feicai922/Pulse-win.git
cd Pulse-win\windows
go test ./...
.\build.ps1
```

`.\build.ps1` automatically runs:
1. `cmd/genrsrc` to generate the multi-resolution `.ico` and PE `.syso` resource files (icon + Per-Monitor V2 DPI manifest) from `AppIcon/pulse-icon-1024.png`;
2. `go build` to produce `Pulse.exe`;
3. `go build -tags installer` to package `Pulse.exe` into `PulseSetup.exe`.

---

## Upstream & Attribution

- Windows port based on [qunqin24/Pulse](https://github.com/qunqin24/Pulse) (original macOS Swift/SwiftUI sources are preserved under `Sources/`).
- UI concept inspired by [**Vinz** (@hivinz_)](https://x.com/hivinz_/status/2092996055248126353).
- Licensed under the [Apache 2.0 License](LICENSE). Third-party notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
