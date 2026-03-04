# Sudoku Desktop Client

[![Latest Release](https://img.shields.io/github/v/release/SUDOKU-ASCII/sudoku-desktop?style=for-the-badge)](https://github.com/SUDOKU-ASCII/sudoku-desktop/releases)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg?style=for-the-badge)](./LICENSE)

Desktop Sudoku game built with `Wails3` + `Vue`.

[中文说明](./README.zh-CN.md)

## Highlights

- Sudoku-focused desktop experience with built-in mini games.
- Supports `sudoku://` protocol only.
- Strict protection against DNS leaks and DNS pollution.
- Uses `hev-tunnel-socks` for cross-platform TUN support in broader proxy scenarios.
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

When enabling `TUN` on macOS, the app asks for your macOS login password **inside the app** (once per app session) to grant admin privileges needed for route/DNS updates. After that, TUN start/stop is silent (no extra system password dialog). The password stays in memory only and is never written to disk.

## License

GPL-3.0
