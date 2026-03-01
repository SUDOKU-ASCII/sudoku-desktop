# Sudoku Desktop (Wails + Vue3 + TypeScript)

跨平台桌面代理客户端，内核使用 [SUDOKU-ASCII/sudoku](https://github.com/SUDOKU-ASCII/sudoku)，TUN 使用 [heiher/hev-socks5-tunnel](https://github.com/heiher/hev-socks5-tunnel)。

## 当前实现范围

- 基础能力
  - `sudoku` 核心进程托管（启动/停止/重启）
  - `hev-socks5-tunnel` 进程托管（可选启用 TUN）
  - 配置持久化（`~/Library/Application Support` 或等效 `UserConfigDir`）
  - 节点管理、短链接导入/导出（`sudoku://`）
  - 节点切换（运行中切换会重启核心）
  - 延迟探测（真连接 `https://i.ytimg.com/generate_204`）
  - 自动选择最低延迟节点
  - 反向代理本地转发器（`-rev-dial/-rev-listen`）
  - 本地端口转发规则（TCP）
  - 自定义 PAC 规则（本地规则追加到 PAC URL 列表）

- 监控与观测
  - 日志采集与分级过滤
  - 活动连接列表（按日志路由决策提取）
  - 实时带宽曲线（按 TUN 网卡采样，显示 Direct/Proxy 估算）
  - 流量分流占比（基于 DIRECT/PROXY 决策做估算）
  - 历史用量统计（按日累计，保存到本地）
  - 直连/代理 IP 检测

- UI
  - Neo-brutalism 风格
  - 亮/暗色自动适配（可手动覆盖）
  - 中/英/俄多语言（可随系统或手动）
  - 页面：Dashboard / Nodes / Routing / TUN / Forwards / Reverse / Logs

## 目录说明

- `internal/core/`：核心后端（配置、进程、探测、监控、事件）
- `frontend/src/`：前端页面与组件
- `scripts/`：内核与构建辅助脚本

## 依赖

- Go `>= 1.26`
- Node.js `>= 18`
- Wails CLI v2
- `sudoku` 可执行文件
- `hev-socks5-tunnel` 可执行文件

## 快速开始

1. 安装前端依赖

```bash
cd frontend
npm install
cd ..
```

也可以使用 Makefile（推荐）：

```bash
make dev
```

2. 构建 `sudoku` 多平台二进制

```bash
./scripts/build_sudoku_matrix.sh
```

产物默认放到：`runtime/bin/<os>-<arch>/sudoku(.exe)`

3. 构建当前主机平台的 `hev-socks5-tunnel`

```bash
./scripts/build_hev_host.sh
```

产物默认放到：`runtime/bin/<os>-<arch>/hev-socks5-tunnel(.exe)`

4. 启动开发

```bash
wails dev
```

## 打包

```bash
./scripts/build_wails_targets.sh
```

该脚本默认只打当前主机平台；跨平台产物建议在对应系统环境分别执行（Windows / macOS amd64 / macOS arm64 / Linux）。

本地一键构建当前平台（包含内核二进制并自动打包进产物）：

```bash
make build
```

## CI 发版与签名

推送 `v` 开头的 tag（例如 `v0.1.0`）会触发 GitHub Actions 跨平台构建，并自动创建 GitHub Release。

Release 会包含：

- 对应平台的压缩包（macOS `.zip`、Windows `.zip`、Linux `.tar.gz`）
- `SHA256SUMS.txt` 校验文件
- 构建来源证明（GitHub Attestations）

如需对校验文件做稳定签名（可用于离线校验），请在本地生成 `cosign` 密钥并配置仓库 Secrets：

1. 生成密钥对（会提示你输入密码）

```bash
cosign generate-key-pair
```

2. 生成 `KEYSTORE_BASE64`（复制输出到 GitHub 仓库 Secrets）

```bash
python3 scripts/print_file_base64.py cosign.key
```

3. 在 GitHub 仓库 Secrets 中设置：

- `KEYSTORE_BASE64`：上一步输出
- `KEYSTORE_PASSWORD`：生成密钥时输入的密码

CI 会自动在 Release 中额外上传：

- `cosign.pub`
- `SHA256SUMS.txt.sig`

校验示例：

```bash
cosign verify-blob --key cosign.pub --signature SHA256SUMS.txt.sig SHA256SUMS.txt
```

注意：上述签名用于发布资产完整性校验，不等同于 macOS/Windows 的系统级代码签名（后者需要 Apple Developer ID / Authenticode 证书）。

## 权限说明（重要）

启用 TUN 时需要管理员权限（路由与网卡配置）。

- Linux 使用 `ip rule/ip route/sysctl`
- macOS 使用 `route`
- Windows 使用 `route` 和 `PowerShell Get-NetIPInterface`

Windows 打包版本默认请求管理员权限启动（UAC）。Linux/macOS 在执行路由/TUN 相关命令时会触发系统提权弹窗（`pkexec` / `osascript`），若系统缺少 Polkit 或提权失败，则会在状态中标记 `needsAdmin=true` 并返回错误。

## 配置路径

应用配置：

- `$(UserConfigDir)/sudoku-desktop/config.json`

运行时文件：

- `$(UserConfigDir)/sudoku-desktop/runtime/`

## 已知限制

- 当前 DIRECT/PROXY 流量拆分为估算值（依据路由决策统计，而非内核逐字节记账）。
- Sudoku 内核本身本地监听策略决定 LAN 实际暴露，若需严格限制请配合系统防火墙。
- 历史用量仅统计应用运行期间的增量（不包含应用未运行时段）。
