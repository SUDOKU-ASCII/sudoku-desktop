# Sudoku Desktop Client

A cross-platform desktop proxy client powered by [SUDOKU-ASCII/sudoku](https://github.com/SUDOKU-ASCII/sudoku) as the core, with optional TUN support via [heiher/hev-socks5-tunnel](https://github.com/heiher/hev-socks5-tunnel).

## Current Scope

- Core capabilities
  - `sudoku` core process management (start/stop/restart)
  - `hev-socks5-tunnel` process management (optional TUN enablement)
  - Persistent configuration storage (`~/Library/Application Support` or equivalent `UserConfigDir`)
  - Node management, short-link import/export (`sudoku://`)
  - Node switching (switching while running restarts the core)
  - Latency probing (real connection to `https://i.ytimg.com/generate_204`)
  - Auto-select the lowest-latency node
  - Reverse proxy local forwarder (`-rev-dial/-rev-listen`)
  - Local port-forwarding rules (TCP)
  - Custom PAC rules (append local rules to the PAC URL list)

- Monitoring and observability
  - Log collection and level-based filtering
  - Active connection list (extracted from log-based routing decisions)
  - Real-time bandwidth charts (sampled from the TUN interface, showing estimated Direct/Proxy)
  - Traffic split ratio (estimated from DIRECT/PROXY decisions)
  - Historical usage statistics (daily totals, stored locally)
  - Direct/Proxy IP detection

- UI
  - Neo-brutalism style
  - Automatic light/dark adaptation (manual override supported)
  - Multilingual: Chinese / English / Russian (system-following or manual)
  - Pages: Dashboard / Game / Nodes / Routing / TUN / Forwards / Reverse / Logs
  - Built-in Sudoku mini-game: 4×4 / 9×9, difficulty selection and hints

## License

GPL