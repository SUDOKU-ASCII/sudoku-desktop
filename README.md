# Sudoku Desktop Client

A cross-platform desktop proxy client powered by [SUDOKU-ASCII/sudoku](https://github.com/SUDOKU-ASCII/sudoku) as the core, with optional TUN support via [heiher/hev-socks5-tunnel](https://github.com/heiher/hev-socks5-tunnel).

## Current Scope

- Core capabilities
  - `sudoku` core process management (start/stop/restart)
  - `hev-socks5-tunnel` process management (optional TUN enablement)
  - Cross-platform TUN safety guards (Windows physical-route pinning/recovery + hidden admin route script invocation; Linux optional netfilter capability degradation)
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

## macOS TUN notes

- Default behavior is **strict-route**: TUN captures traffic and split routing is decided by the `sudoku` core (PAC/rules), not by system-level CIDR pre-splitting.
- Optional legacy behavior: set `SUDOKU_DARWIN_ENABLE_CIDR_BYPASS=1` to enable CIDR bypass pre-split in PAC mode.

## GitHub Release (macOS direct-run)

To make macOS release zips runnable right after download/unzip (without `xattr -cr`), the release workflow now performs:

- `codesign` (Developer ID Application)
- Apple notarization (`notarytool`)
- stapling notarization ticket (`stapler`)

Required GitHub Action secrets:

- `MACOS_CERT_P12_BASE64`: Base64 of the Developer ID Application `.p12`
- `MACOS_CERT_P12_PASSWORD`: Password for the `.p12`
- `MACOS_CERT_IDENTITY`: Signing identity (optional, auto-detected if empty)
- `MACOS_NOTARY_KEY_ID`: App Store Connect API key ID
- `MACOS_NOTARY_ISSUER_ID`: App Store Connect issuer ID
- `MACOS_NOTARY_API_KEY_P8_BASE64`: Base64 of `AuthKey_<KEY_ID>.p8`

## License

GPL
