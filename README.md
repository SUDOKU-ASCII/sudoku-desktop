# Sudoku Desktop Client

[![Latest Release](https://img.shields.io/github/v/release/SUDOKU-ASCII/sudoku-desktop?style=for-the-badge)](https://github.com/SUDOKU-ASCII/sudoku-desktop/releases)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg?style=for-the-badge)](./LICENSE)

Desktop Sudoku game built with `Wails3` + `Vue`.

[中文说明](./README.zh-CN.md)

## Highlights

- Sudoku-focused desktop experience with built-in mini games.
- Supports `sudoku://` protocol only.
- Strict protection against DNS leaks and DNS pollution.
- Uses [hev-socks5-tunnel](https://github.com/heiher/hev-socks5-tunnel) for cross-platform TUN support in broader proxy scenarios.
- Forces IPv4 preference to avoid common IPv6 compatibility issues.
- Multi-language UI and light/dark theme support.

## macOS First Run (Important)

If macOS blocks the app after unzip/download, clear quarantine attributes first.

1. Open `Terminal`.
2. Run:

```bash
xattr -cr "/Applications/sudoku4x4.app"
```

If your app is not in `/Applications`, replace the path with the actual `.app` path.
Tip: Type `xattr -cr ` first, then drag the `.app` file into Terminal to auto-fill the path.

## Proxy, controller and TUN

- The app always listens on `127.0.0.1:10233` (mixed HTTP/SOCKS5) and uses it as the system proxy; the port is fixed. The proxy is loopback-only by default. **Allow LAN connections** in Settings opens it to other devices; this needs a Sudoku core with `local_address` support.
- The bundled stock Sudoku core exposes a Clash/mihomo-compatible **external controller** (default `127.0.0.1:9097`, with a secret). Set the port, secret and LAN access in Settings, then open Zashboard / MetaCubeXD / Yacd from the app to control the core from a web dashboard.
- Routing mode (PAC / Global / Direct) and node selection (including the `Balance` group) switch live through the controller, with no restart.
- `TUN` runs in a separate elevated helper. On macOS and Linux the app asks for your administrator password in its own dialog. The password stays in memory only and no system prompt appears. Windows uses UAC. The GUI never runs as root.
- If TUN fails to start, the whole session rolls back: the core stops and the system proxy is restored. When TUN is turned off, the helper exits immediately.
- The helper tears routes, DNS and the tunnel down if the app quits, crashes or is killed, and a user-level guard restores the system proxy. If anything is ever left behind, use **Repair network** in the app.
- On Windows the WinHTTP proxy is synced together with the system proxy whenever the app has administrator rights.

## License

GPL-3.0
