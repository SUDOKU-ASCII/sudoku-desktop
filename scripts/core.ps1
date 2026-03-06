param(
  [ValidateSet("all", "sudoku", "hev")]
  [string]$What = "all"
)

$ErrorActionPreference = "Stop"

function Get-RepoRoot {
  $here = $PSScriptRoot
  if (-not $here) {
    $here = Split-Path -Parent $PSCommandPath
  }
  if (-not $here) {
    $here = (Get-Location).Path
  }
  return (Resolve-Path (Join-Path $here "..")).Path
}

function Ensure-Dir([string]$Path) {
  if (-not (Test-Path -LiteralPath $Path)) {
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
  }
}

function New-TempDir([string]$Prefix) {
  $p = Join-Path $env:TEMP ($Prefix + [Guid]::NewGuid().ToString("N"))
  New-Item -ItemType Directory -Force -Path $p | Out-Null
  return $p
}

function Get-NewLine([string]$Text) {
  if ($Text -like "*`r`n*") { return "`r`n" }
  return "`n"
}

$utf8NoBom = New-Object System.Text.UTF8Encoding $false

function Read-Text([string]$Path) {
  return [System.IO.File]::ReadAllText($Path, [System.Text.Encoding]::UTF8)
}

function Write-Text([string]$Path, [string]$Text) {
  [System.IO.File]::WriteAllText($Path, $Text, $script:utf8NoBom)
}

function Replace-First([string]$Text, [string]$Needle, [string]$Replacement) {
  $idx = $Text.IndexOf($Needle)
  if ($idx -lt 0) { return $null }
  return $Text.Substring(0, $idx) + $Replacement + $Text.Substring($idx + $Needle.Length)
}

function Replace-FirstN([string]$Text, [string]$Needle, [string]$Replacement, [int]$Count) {
  $out = $Text
  for ($i = 0; $i -lt $Count; $i++) {
    $next = Replace-First $out $Needle $Replacement
    if ($null -eq $next) { return $null }
    $out = $next
  }
  return $out
}

function Patch-ReplaceFirst([string]$Path, [string]$Needle, [string]$Replacement) {
  $data = Read-Text $Path
  if ($data.Contains($Replacement)) { return }
  if (-not $data.Contains($Needle)) { throw "patch failed: needle not found in $Path" }
  $new = Replace-First $data $Needle $Replacement
  if ($null -eq $new) { throw "patch failed: replace failed in $Path" }
  Write-Text $Path $new
}

function Patch-ReplaceFirstN([string]$Path, [string]$Needle, [string]$Replacement, [int]$Count) {
  $data = Read-Text $Path
  if ($data.Contains($Replacement)) { return }
  if (-not $data.Contains($Needle)) { throw "patch failed: needle not found in $Path" }
  $new = Replace-FirstN $data $Needle $Replacement $Count
  if ($null -eq $new) { throw "patch failed: replace failed in $Path" }
  Write-Text $Path $new
}

function Patch-GoDialTarget([string]$Path) {
  $data = Read-Text $Path
  $needle = "func dialTarget("
  $start = $data.IndexOf($needle)
  if ($start -lt 0) { throw "dialTarget not found in $Path (upstream changed?)" }
  $braceStart = $data.IndexOf("{", $start)
  if ($braceStart -lt 0) { throw "dialTarget brace not found in $Path" }
  $level = 0
  $end = -1
  for ($i = $braceStart; $i -lt $data.Length; $i++) {
    $ch = $data[$i]
    if ($ch -eq "{") {
      $level++
    } elseif ($ch -eq "}") {
      $level--
      if ($level -eq 0) {
        $end = $i + 1
        break
      }
    }
  }
  if ($end -lt 0) { throw "dialTarget end not found in $Path" }
  $funcText = $data.Substring($start, $end - $start)
  if ($funcText.Contains("wrapConnForTrafficStats")) { return }
  $patched = Replace-First $funcText "return conn, true" "return wrapConnForTrafficStats(conn, true), true"
  if ($null -eq $patched) { throw "patch failed: return conn, true not found in $Path" }
  $patched = Replace-First $patched "return dConn, true" "return wrapConnForTrafficStats(dConn, false), true"
  if ($null -eq $patched) { throw "patch failed: return dConn, true not found in $Path" }
  $out = $data.Substring(0, $start) + $patched + $data.Substring($end)
  Write-Text $Path $out
}

function Invoke-Native([string]$FilePath, [string[]]$ArgumentList) {
  & $FilePath @ArgumentList
  if ($LASTEXITCODE -ne 0) {
    $args = ($ArgumentList -join " ")
    throw "command failed (exit=$LASTEXITCODE): $FilePath $args"
  }
}

$root = Get-RepoRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  throw "go not found in PATH"
}

$goos = (& go env GOOS).Trim()
$goarch = (& go env GOARCH).Trim()
$platformDir = "${goos}-${goarch}"

$runtimeBinRoot = Join-Path $root "runtime/bin"
$outDir = Join-Path $runtimeBinRoot $platformDir

Ensure-Dir $runtimeBinRoot
Ensure-Dir $outDir

Get-ChildItem -Path $runtimeBinRoot -Directory -ErrorAction SilentlyContinue |
  Where-Object { $_.Name -ne $platformDir } |
  ForEach-Object { Remove-Item -Recurse -Force -LiteralPath $_.FullName }

#
# 1) Fetch HEV release (Windows host)
#
if ($What -eq "all" -or $What -eq "hev") {
  $hevVersion = $env:HEV_VERSION
  if (-not $hevVersion) { $hevVersion = "2.14.4" }

  if ($goos -eq "windows" -and $goarch -eq "amd64") {
    $asset = "hev-socks5-tunnel-win64.zip"
  } else {
    throw "Unsupported platform for scripts/core.ps1: ${goos}/${goarch} (use scripts/fetch_hev_release.sh on non-Windows hosts)"
  }

  $hevUrl = "https://github.com/heiher/hev-socks5-tunnel/releases/download/${hevVersion}/${asset}"
  $hevTmp = New-TempDir "hev-"
  try {
    $zipPath = Join-Path $hevTmp $asset
    Invoke-WebRequest -Uri $hevUrl -OutFile $zipPath | Out-Null
    Expand-Archive -Path $zipPath -DestinationPath $hevTmp -Force
    $srcDir = Join-Path $hevTmp "hev-socks5-tunnel"
    if (-not (Test-Path -LiteralPath $srcDir)) {
      throw "HEV zip layout changed; missing folder: $srcDir"
    }
    Copy-Item -Force -Path (Join-Path $srcDir "*") -Destination $outDir
  } finally {
    Remove-Item -Recurse -Force -LiteralPath $hevTmp -ErrorAction SilentlyContinue
  }

  foreach ($dep in @("hev-socks5-tunnel.exe", "wintun.dll", "msys-2.0.dll")) {
    $p = Join-Path $outDir $dep
    if (-not (Test-Path -LiteralPath $p)) {
      throw "Missing HEV dependency after extraction: $p"
    }
  }
}

#
# 2) Fetch + patch + build sudoku core
#
if ($What -eq "all" -or $What -eq "sudoku") {
  $sudokuRepo = $env:SUDOKU_REPO
  if (-not $sudokuRepo) { $sudokuRepo = "https://github.com/SUDOKU-ASCII/sudoku.git" }
  $sudokuRef = $env:SUDOKU_REF
  if (-not $sudokuRef) { $sudokuRef = "v0.3.2" }

  $tmp = New-TempDir "sudoku-build-"
  $sudokuDir = Join-Path $tmp "sudoku"
  try {
    $git = Get-Command git -ErrorAction SilentlyContinue
    $cloned = $false
    if ($git) {
      try {
        Invoke-Native $git.Source @("clone", "--depth", "1", "--branch", $sudokuRef, $sudokuRepo, $sudokuDir)
        $cloned = $true
      } catch {
        $cloned = $false
      }
    }

    if (-not $cloned) {
      $tar = Get-Command tar -ErrorAction SilentlyContinue
      if (-not $tar) { throw "git clone failed and 'tar' not found for tarball fallback" }
      Ensure-Dir $sudokuDir
      $tarUrl = "https://codeload.github.com/SUDOKU-ASCII/sudoku/tar.gz/${sudokuRef}"
      $tgzPath = Join-Path $tmp "sudoku.tar.gz"
      Invoke-WebRequest -Uri $tarUrl -OutFile $tgzPath | Out-Null
      Invoke-Native $tar.Source @("-xzf", $tgzPath, "-C", $sudokuDir, "--strip-components=1")
    }

    # Relax upstream go.mod patch version (go 1.26.0 -> go 1.26) for toolchain compatibility.
    $goMod = Join-Path $sudokuDir "go.mod"
    if (Test-Path -LiteralPath $goMod) {
      $data = Read-Text $goMod
      $new = [Regex]::Replace($data, "(?m)^go\\s+(\\d+)\\.(\\d+)\\.\\d+\\s*$", { param($m) "go $($m.Groups[1].Value).$($m.Groups[2].Value)" })
      if ($new -ne $data) {
        Write-Text $goMod $new
      }
    }

    # Overlay patch tree into upstream repo.
    $patchDir = Join-Path $root "scripts/sudoku_patches"
    if (Test-Path -LiteralPath $patchDir) {
      Copy-Item -Recurse -Force -Path (Join-Path $patchDir "*") -Destination $sudokuDir
    }

    # Patch dialTarget() to wrap conns for traffic stats (direct/proxy).
    Patch-GoDialTarget (Join-Path $sudokuDir "internal/app/client_target.go")

    # Patch SOCKS5 UDP associate DIRECT path to avoid TUN self-loop for outbound UDP.
    $socks5 = Join-Path $sudokuDir "internal/app/client_socks5.go"
    $socksData = Read-Text $socks5
    if (-not $socksData.Contains("udpWriteTo(")) {
      Patch-ReplaceFirst $socks5 "s.udpConn.WriteToUDP(payload, directAddr)" "udpWriteTo(s.udpConn, payload, directAddr, true)"
      Patch-ReplaceFirstN $socks5 "s.udpConn.WriteToUDP(resp, clientAddr)" "udpWriteTo(s.udpConn, resp, clientAddr, false)" 2
    }

    $out = Join-Path $outDir ("sudoku" + $(if ($goos -eq "windows") { ".exe" } else { "" }))
    Push-Location $sudokuDir
    try {
      $env:CGO_ENABLED = "0"
      $env:GOOS = $goos
      $env:GOARCH = $goarch
      Invoke-Native "go" @("build", "-mod=mod", "-tags", "sudoku_patch", "-trimpath", "-ldflags", "-s -w", "-o", $out, "./cmd/sudoku-tunnel")
    } finally {
      Pop-Location
    }
  } finally {
    Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction SilentlyContinue
  }
}

Write-Host "[ok] core binaries ready at $outDir"
