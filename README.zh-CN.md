# Sudoku Desktop Client

[![Latest Release](https://img.shields.io/github/v/release/SUDOKU-ASCII/sudoku-desktop?style=for-the-badge)](https://github.com/SUDOKU-ASCII/sudoku-desktop/releases)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg?style=for-the-badge)](./LICENSE)

一个基于 `Wails3` + `Vue` 的桌面数独游戏。

English version: [README.md](./README.md)

## 项目亮点

- 专注数独体验，同时内置小游戏。
- 仅支持 `sudoku://` 协议。
- 严格防护 DNS 泄露与 DNS 污染。
- 使用 [hev-socks5-tunnel](https://github.com/heiher/hev-socks5-tunnel) 提供全平台 TUN 适配，覆盖更广泛的代理场景。
- 强制 IPv4 优先，规避常见 IPv6 兼容问题。
- 支持多语言界面与深浅色主题。

## macOS 首次运行（重要）

如果你下载或解压后无法直接打开应用，请先清理 macOS 隔离属性。

1. 打开 `终端`。
2. 执行：

```bash
xattr -cr "/Applications/sudoku4x4.app"
```

如果你的应用不在 `/Applications`，请把命令中的路径替换为实际 `.app` 路径。
提示：可先输入 `xattr -cr `（末尾留空格），再将 `.app` 文件拖入终端自动补全路径。

在 macOS 上启用 `TUN` 时，应用会在**软件内**要求输入 macOS 登录密码（每次打开软件仅一次），用于获取修改路由 / DNS 所需的管理员权限。之后 TUN 启停为静默模式（不会重复弹出系统级密码框）。密码仅保存在内存中，不会写入磁盘。

## 许可证

GPL-3.0
