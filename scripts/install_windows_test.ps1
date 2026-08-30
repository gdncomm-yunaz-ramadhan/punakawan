<#
.SYNOPSIS
  Windows-specific checks for scripts/install.ps1: it must parse cleanly,
  and a dry run must describe deploying adapters and configuring
  credentials/hooks below the resolved data directory - never a path
  inside this checkout - so a relocated/overridden install prefix cannot
  silently point back at the source clone.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$InstallScript = Join-Path $ScriptDir 'install.ps1'

function Assert-Contains {
    param([string]$Output, [string]$Expected, [string]$Message)
    if ($Output -notlike "*$Expected*") {
        throw "FAIL: $Message`nExpected to find: $Expected"
    }
}

function Assert-NotContains {
    param([string]$Output, [string]$Unexpected, [string]$Message)
    if ($Output -like "*$Unexpected*") {
        throw "FAIL: $Message`nDid not expect to find: $Unexpected"
    }
}

Write-Host '==> Parsing scripts/install.ps1'
$parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile($InstallScript, [ref]$null, [ref]$parseErrors)
if ($parseErrors -and $parseErrors.Count -gt 0) {
    throw "FAIL: install.ps1 has $($parseErrors.Count) parse error(s):`n$($parseErrors -join "`n")"
}

Write-Host '==> Dry run into a relocated, isolated data/install prefix'
$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("punakawan-install-test-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $testRoot | Out-Null
try {
    $dataDir = Join-Path $testRoot 'data'
    $installDir = Join-Path $testRoot 'bin'

    $env:PUNAKAWAN_DATA_DIR = $dataDir
    $env:PUNAKAWAN_INSTALL_DIR = $installDir
    $env:PUNAKAWAN_CODEX_BIN = Join-Path $testRoot 'no-such-codex.exe'
    $env:PUNAKAWAN_CLAUDE_BIN = Join-Path $testRoot 'no-such-claude.exe'
    try {
        $output = & $InstallScript -DryRun 2>&1 | Out-String
    } finally {
        Remove-Item Env:\PUNAKAWAN_DATA_DIR -ErrorAction SilentlyContinue
        Remove-Item Env:\PUNAKAWAN_INSTALL_DIR -ErrorAction SilentlyContinue
        Remove-Item Env:\PUNAKAWAN_CODEX_BIN -ErrorAction SilentlyContinue
        Remove-Item Env:\PUNAKAWAN_CLAUDE_BIN -ErrorAction SilentlyContinue
    }

    Assert-Contains $output 'pnpm --filter @punakawan/schema-types build' 'schema-types builds before adapters deploy'
    Assert-Contains $output 'pnpm --filter @punakawan/adapter-sdk build' 'adapter-sdk builds before adapters deploy'
    Assert-Contains $output 'pnpm --filter @punakawan/adapter-atlassian build' 'atlassian adapter builds before it deploys'
    Assert-Contains $output 'pnpm --filter @punakawan/github-adapter build' 'github adapter builds before it deploys'
    Assert-Contains $output 'go install ./cmd/punakawan ./cmd/punakawand' 'go install runs'
    Assert-Contains $output ([regex]::Escape((Join-Path $dataDir 'adapters\atlassian'))) 'atlassian adapter deploys below the relocated data directory'
    Assert-Contains $output ([regex]::Escape((Join-Path $dataDir 'adapters\github'))) 'github adapter deploys below the relocated data directory'
    Assert-Contains $output 'setup --hooks-only' 'lifecycle telemetry hooks are configured via setup --hooks-only'
    Assert-Contains $output 'mcp add punakawan' 'MCP registration is attempted'
    Assert-Contains $output 'Generic MCP config' 'generic MCP config is described'
    Assert-Contains $output 'Configuring global adapters' 'global adapter config is described'
    Assert-Contains $output 'punakawan panel' 'closing summary mentions the panel command'

    $repoRootEscaped = [regex]::Escape((Split-Path -Parent $ScriptDir))
    Assert-NotContains $output "packages\\adapter-atlassian\\dist" 'dry run must never reference the checkout''s own packages/adapter-atlassian/dist'
    Assert-NotContains $output "packages\\github-adapter\\dist" 'dry run must never reference the checkout''s own packages/github-adapter/dist'
} finally {
    Remove-Item -Recurse -Force $testRoot -ErrorAction SilentlyContinue
}

Write-Host 'installer checks passed'
