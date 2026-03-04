# Sudoku Desktop Client

[![Latest Release](https://img.shields.io/github/v/release/SUDOKU-ASCII/sudoku-desktop?style=for-the-badge)](https://github.com/SUDOKU-ASCII/sudoku-desktop/releases)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg?style=for-the-badge)](./LICENSE)

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

When enabling `TUN` on macOS, the app will ask for your macOS login password **inside the app** (once per app session) to grant administrator privileges required to modify routes/DNS. After that, TUN start/stop is silent (no macOS system password dialog). The password is kept in memory only and never written to disk.

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

在 macOS 上启用 `TUN` 开关时，应用会在**软件内**弹窗要求输入 macOS 登录密码（每次打开软件仅需一次），用于获取修改系统路由 / DNS 所需的管理员权限。之后的 TUN 启停为静默启停（不会再弹 macOS 系统级密码框）。密码仅保存在内存中，不会写入磁盘。

## What is this project?

This project is a Wails-based desktop Sudoku game.

- Built with `Wails3` + `Vue`
- Built-in Sudoku gameplay (4x4 / 9x9)
- Difficulty selection, hints, and basic game controls
- Best user experience should be unmatched

## 这啥？

当前项目为一个基于 Wails3 的桌面数独游戏。

- 使用 `Wails3` + `Vue` 构建
- 内置数独玩法（4x4 / 9x9）
- 提供难度选择、提示和基础操作
- 用户体验应当无敌



## License

GPL
