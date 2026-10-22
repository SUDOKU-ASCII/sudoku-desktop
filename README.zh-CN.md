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

## 代理、控制器与 TUN

- 应用固定监听 `127.0.0.1:10233`（HTTP/SOCKS5 混合端口）并将其设为系统代理，端口不可更改。代理默认只在本机回环地址监听。在设置中开启「允许局域网连接」后，局域网设备也能使用，前提是 Sudoku 内核支持 `local_address`。
- 内置的原版 Sudoku 内核提供兼容 Clash/mihomo 的**外部控制器**（默认 `127.0.0.1:9097`，带密钥）。可在设置中修改端口、密钥与局域网访问，并在应用内一键打开 Zashboard / MetaCubeXD / Yacd 进行网页面板控制。
- 路由模式（PAC / 全局 / 直连）与节点选择（含 `Balance` 负载均衡组）均通过控制器实时切换，无需重启。
- `TUN` 由独立的提权助手进程承担。在 macOS 和 Linux 上，应用通过软件内的对话框输入管理员密码，密码只保存在内存中，不会弹出系统授权窗口；Windows 使用 UAC。GUI 本身从不以 root 运行。
- TUN 启动失败时会整体回滚：停止内核并恢复系统代理。关闭 TUN 时，助手进程会立即退出。
- 应用退出、崩溃或被强杀时，助手会自动清理路由、DNS 与隧道，用户态守护进程负责恢复系统代理。若仍有残留，可在应用内点击「修复网络」。
- 在 Windows 上，只要应用具有管理员权限，WinHTTP 代理会与系统代理一起同步。

## 许可证

GPL-3.0
