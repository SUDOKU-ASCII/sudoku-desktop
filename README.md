# Sudoku Desktop Client

## macOS First Run (Important)

If macOS blocks the app after you unzip/download it, clear quarantine attributes first.

1. Open `Terminal`.
2. Run:

```bash
xattr -cr "/Applications/sudoku4x4.app"
```

If your app is not in `/Applications`, replace the path with the real `.app` path.
Beginner tip: type `xattr -cr ` first, then drag the `.app` file into the Terminal window to auto-fill the path.

Then start the app normally.

When you start or stop `TUN` on macOS, the system may ask for your login password. This is expected because the app is modifying network routes/interfaces. Please enter your password and allow it.

## macOS 首次运行（重要）

如果你下载或解压后无法直接打开应用，请先清理 macOS 隔离属性。

1. 打开 `终端`。
2. 执行：

```bash
xattr -cr "/Applications/sudoku4x4.app"
```

如果你的应用不在 `/Applications`，请把命令里的路径改成实际 `.app` 路径。
小白提示：你可以先输入 `xattr -cr `（末尾保留空格），再把 `.app` 文件拖进终端窗口自动补全路径。

然后再正常双击启动。

在 macOS 上启用或停止 `TUN` 时，系统可能会弹出密码输入框。这是正常行为，因为应用需要修改网络路由和网卡配置。请放心输入密码并授权。

## Current Scope

This project is currently positioned as a Wails-based desktop Sudoku game.

- Built with `Wails` + `Vue`
- Built-in Sudoku gameplay (4x4 / 9x9)
- Difficulty selection, hints, and basic game controls
- Cross-platform desktop packaging via GitHub Actions

## 当前定位

当前项目定位为一个基于 Wails 的桌面数独游戏。

- 使用 `Wails` + `Vue` 构建
- 内置数独玩法（4x4 / 9x9）
- 提供难度选择、提示和基础操作
- 通过 GitHub Actions 进行跨平台桌面打包

## GitHub Release CI

The release workflow builds artifacts for macOS / Windows / Linux.

No Apple signing/notarization keys are required by default.

Optional signing:

- `KEYSTORE_BASE64`
- `KEYSTORE_PASSWORD`

If these two are not provided, CI still builds and publishes release artifacts, but checksum signature files are skipped.

## GitHub 发布说明

发布工作流会构建 macOS / Windows / Linux 的安装包。

默认不再要求 Apple 签名与公证相关密钥。

可选签名参数：

- `KEYSTORE_BASE64`
- `KEYSTORE_PASSWORD`

如果不配置这两个参数，CI 仍会正常构建并发布产物，但会跳过校验文件签名步骤。

## License

GPL
