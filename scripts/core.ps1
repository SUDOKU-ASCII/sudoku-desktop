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
  if (-not $hevVersion) { $hevVersion = "2.17.1" }

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
# 2) Build the stock upstream sudoku core pinned in go.mod (no patching)
#
if ($What -eq "all" -or $What -eq "sudoku") {
  $out = Join-Path $outDir ("sudoku" + $(if ($goos -eq "windows") { ".exe" } else { "" }))
  $ver = (& go list -m -f '{{.Version}}' github.com/SUDOKU-ASCII/sudoku).Trim()
  if ($LASTEXITCODE -ne 0 -or -not $ver) { throw "cannot resolve sudoku version from go.mod" }
  $tmpGo = New-TempDir "sudoku-build-"
  try {
    # `go install pkg@version` uses upstream's own go.mod, keeping its CLI-only deps out of this module.
    $env:GOPATH = Join-Path $tmpGo "gopath"
    $env:GOMODCACHE = (& go env GOMODCACHE).Trim()
    $env:GOFLAGS = "-mod=mod"
    $env:CGO_ENABLED = "0"
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    Push-Location $tmpGo
    try {
      Invoke-Native "go" @("install", "-trimpath", "-ldflags", "-s -w", "github.com/SUDOKU-ASCII/sudoku/cmd/sudoku-tunnel@$ver")
    } finally {
      Pop-Location
    }
    $built = Get-ChildItem -Path (Join-Path $env:GOPATH "bin") -Recurse -File | Where-Object { $_.Name -like "sudoku-tunnel*" } | Select-Object -First 1
    if (-not $built) { throw "build produced no binary" }
    Copy-Item -Force -LiteralPath $built.FullName -Destination $out
  } finally {
    Remove-Item -Recurse -Force -LiteralPath $tmpGo -ErrorAction SilentlyContinue
  }
}

Write-Host "[ok] core binaries ready at $outDir"
