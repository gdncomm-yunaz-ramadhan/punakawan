#Requires -Version 5.1
<#
.SYNOPSIS
  Punakawan installer for Windows (PowerShell equivalent of scripts/install.sh).

.DESCRIPTION
  Mirrors the macOS installer's steps for Windows:
    1. Verify / install prerequisites (via winget where available).
    2. Build Punakawan once from this checkout (go build + pnpm build).
    3. Place the punakawan binary on the user's PATH.
    4. Create the global config directory (Go's os.UserConfigDir on Windows:
       %APPDATA%\punakawan) and write Atlassian credentials + adapter config.
    5. Generate an MCP launcher (run-mcp.cmd) and print client-registration
       instructions.
    6. Run `punakawan doctor`.

  Unlike the macOS script this installer does not drive the interactive
  agent-client wizard (scripts/configure-agent.sh is bash-only); it writes the
  generic MCP config and prints the command to register it manually.

.NOTES
  Run from anywhere:  powershell -ExecutionPolicy Bypass -File scripts\install.ps1
  Non-interactive credential entry can be pre-seeded via the environment
  variables ATLASSIAN_API_TOKEN / ATLASSIAN_HOST / ATLASSIAN_EMAIL /
  ATLASSIAN_API_TOKEN_SCOPED before running.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot  = Split-Path -Parent $ScriptDir

function Write-Step { param([string]$Message) Write-Host "`n==> $Message" -ForegroundColor Cyan }
function Write-Warn { param([string]$Message) Write-Warning $Message }

function Test-Command { param([string]$Name) return [bool](Get-Command $Name -ErrorAction SilentlyContinue) }

# --- 1. Prerequisites -------------------------------------------------------
# On Windows the canonical package manager is winget. Each tool is best-effort:
# a failed install warns and continues so the user can install it by hand.

function Install-IfMissing {
    param(
        [string]$Command,   # command to probe on PATH
        [string]$WingetId,  # winget package id
        [switch]$Optional   # optional tools warn instead of failing
    )
    if (Test-Command $Command) {
        Write-Step "$Command already installed ($((Get-Command $Command).Source))"
        return
    }
    if (-not (Test-Command 'winget')) {
        $msg = "$Command is missing and winget is unavailable. Install $Command manually."
        if ($Optional) { Write-Warn $msg } else { throw $msg }
        return
    }
    Write-Step "Installing $WingetId (provides $Command)"
    try {
        winget install --exact --id $WingetId --accept-source-agreements --accept-package-agreements
    } catch {
        $msg = "Failed to install ${WingetId}: $($_.Exception.Message)"
        if ($Optional) { Write-Warn $msg } else { throw $msg }
    }
}

Install-IfMissing -Command git  -WingetId 'Git.Git'
Install-IfMissing -Command rg   -WingetId 'BurntSushi.ripgrep.MSVC'
Install-IfMissing -Command node -WingetId 'OpenJS.NodeJS'
Install-IfMissing -Command go   -WingetId 'GoLang.Go'
Install-IfMissing -Command dolt -WingetId 'DoltHub.Dolt' -Optional
# bd (beads) and rtk do not have well-known winget ids; warn if absent.
if (-not (Test-Command 'bd'))  { Write-Warn "bd (beads) not found; install it manually (see project README)." }
if (-not (Test-Command 'rtk')) { Write-Warn "rtk not found; install it manually if you use the token-optimizing proxy." }

if (-not (Test-Command 'pnpm')) {
    Write-Step 'Installing pnpm (npm install -g pnpm)'
    npm install -g pnpm
} else {
    Write-Step "pnpm already installed ($((Get-Command pnpm).Source))"
}

# --- 2. Build Punakawan (mirrors Makefile bootstrap/build/package) ----------

Write-Step 'Building Punakawan (go build + pnpm build)'
Push-Location $RepoRoot
try {
    go mod download
    pnpm install
    go build ./...
    pnpm -r --if-present build
    # `package` target: emit the two binaries with .exe suffixes.
    New-Item -ItemType Directory -Force -Path (Join-Path $RepoRoot 'dist') | Out-Null
    go build -o (Join-Path $RepoRoot 'dist\punakawan.exe')  ./cmd/punakawan
    go build -o (Join-Path $RepoRoot 'dist\punakawand.exe') ./cmd/punakawand
} finally {
    Pop-Location
}

$PunakawanBin = Join-Path $RepoRoot 'dist\punakawan.exe'
$AdapterEntry = Join-Path $RepoRoot 'packages\adapter-atlassian\dist\run.js'

if (-not (Test-Path $PunakawanBin)) { throw "Build did not produce $PunakawanBin" }
if (-not (Test-Path $AdapterEntry)) { throw "Build did not produce $AdapterEntry" }

# --- 3. Place the binary on PATH -------------------------------------------
# %LOCALAPPDATA%\Programs\punakawan mirrors the macOS ~/.local/bin step.

$LocalBin = Join-Path $env:LOCALAPPDATA 'Programs\punakawan'
New-Item -ItemType Directory -Force -Path $LocalBin | Out-Null
Copy-Item -Force $PunakawanBin (Join-Path $LocalBin 'punakawan.exe')
Write-Step "Copied punakawan.exe -> $LocalBin"

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $LocalBin) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$LocalBin", 'User')
    Write-Step "Added $LocalBin to your user PATH (restart your shell to pick it up)"
} else {
    Write-Step "$LocalBin already on user PATH"
}

# --- 4. Global config location (matches os.UserConfigDir on Windows) --------

$GlobalDir    = Join-Path $env:APPDATA 'punakawan'
New-Item -ItemType Directory -Force -Path $GlobalDir | Out-Null
$GlobalConfig = Join-Path $GlobalDir 'config.yaml'
$GlobalEnv    = Join-Path $GlobalDir '.env'

# --- 4b. Atlassian credentials (written once, globally) ---------------------

if (Test-Path $GlobalEnv) {
    Write-Step "$GlobalEnv already exists, leaving credentials as-is"
} else {
    Write-Step 'Direct Jira REST connection'
    Write-Host @'
Punakawan calls Jira Cloud REST API v3 directly. Rovo MCP is not used.

Choose the token type you created:
  1) Personal API token without scopes (email + site URL)
  2) Personal API token with scopes (email + Atlassian API gateway)
  3) Service-account scoped token (Bearer + Atlassian API gateway)
'@

    $host_    = $env:ATLASSIAN_HOST
    $token    = $env:ATLASSIAN_API_TOKEN
    $email    = $env:ATLASSIAN_EMAIL
    $scoped   = $env:ATLASSIAN_API_TOKEN_SCOPED

    if (-not $token) {
        $choice = Read-Host 'Which do you have? [1/2/3, default 1]'
        if (-not $choice) { $choice = '1' }
        if ($choice -notmatch '^[123]$') { throw "Invalid token choice: $choice" }

        $host_ = Read-Host 'Atlassian site host (e.g. yourteam.atlassian.net)'
        $secure = Read-Host 'Atlassian API token' -AsSecureString
        $token = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
            [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
        if ($choice -ne '3') { $email = Read-Host 'Atlassian account email' }
        $scoped = if ($choice -eq '1') { 'false' } else { 'true' }
    }

    $lines = @(
        "ATLASSIAN_API_TOKEN=$token"
        "ATLASSIAN_API_TOKEN_SCOPED=$scoped"
        "ATLASSIAN_HOST=$host_"
    )
    if ($email) { $lines += "ATLASSIAN_EMAIL=$email" }
    Set-Content -Path $GlobalEnv -Value $lines -Encoding ASCII

    # Restrict the file to the current user (rough equivalent of chmod 600).
    try {
        $acl = New-Object System.Security.AccessControl.FileSecurity
        $acl.SetAccessRuleProtection($true, $false)
        $rule = New-Object System.Security.AccessControl.FileSystemAccessRule(
            [System.Security.Principal.WindowsIdentity]::GetCurrent().Name,
            'FullControl', 'Allow')
        $acl.AddAccessRule($rule)
        Set-Acl -Path $GlobalEnv -AclObject $acl
    } catch {
        Write-Warn "Could not tighten ACL on ${GlobalEnv}: $($_.Exception.Message)"
    }
    Write-Step "Wrote credentials to $GlobalEnv (user-only ACL, outside any git-tracked directory)"
}

# --- 5. Global adapter config (workspace.GlobalConfig) ----------------------

if (Test-Path $GlobalConfig) {
    Write-Step "$GlobalConfig already exists, leaving it as-is"
} else {
    $adapterEntryYaml = $AdapterEntry -replace '\\', '\\'
    @"
adapters:
  atlassian:
    command: node
    args:
      - $adapterEntryYaml
    env_passthrough:
      - ATLASSIAN_API_TOKEN
      - ATLASSIAN_API_TOKEN_SCOPED
      - ATLASSIAN_HOST
      - ATLASSIAN_EMAIL
"@ | Set-Content -Path $GlobalConfig -Encoding ASCII
    Write-Step "Wrote $GlobalConfig"
}
Write-Host 'Any project can still add its own .punakawan\workspace.yaml with an'
Write-Host 'adapters: section to override this - that remains fully optional.'

# --- 6. MCP launcher --------------------------------------------------------
# A .cmd wrapper that loads the global credentials, then execs the MCP server
# from the caller's working directory (so workspace.Discover resolves the
# project the agent client is using).

$RunScript = Join-Path $GlobalDir 'run-mcp.cmd'
@"
@echo off
REM Generated by scripts\install.ps1 - loads global credentials, then runs
REM punakawan's MCP server from the caller's own working directory.
if exist "$GlobalEnv" (
  for /f "usebackq tokens=1,* delims==" %%A in ("$GlobalEnv") do set "%%A=%%B"
)
"$PunakawanBin" mcp serve
"@ | Set-Content -Path $RunScript -Encoding ASCII
Write-Step "Wrote $RunScript"

$GenericConfig = Join-Path $GlobalDir 'mcp-config.json'
@"
{
  "mcpServers": {
    "punakawan": {
      "command": "$($RunScript -replace '\\', '\\')"
    }
  }
}
"@ | Set-Content -Path $GenericConfig -Encoding ASCII
Write-Step "Wrote generic MCP config $GenericConfig"

# --- 7. Verify --------------------------------------------------------------

Write-Step 'Running punakawan doctor'
try { & $PunakawanBin doctor } catch { Write-Warn "doctor reported issues above - resolve before using punakawan" }

Write-Host @"

==> Done.

Binary:        $LocalBin\punakawan.exe (source: $PunakawanBin)
Credentials:   $GlobalEnv (not git-tracked)
Global config: $GlobalConfig
MCP launcher:  $RunScript

Register the "punakawan" MCP server with your agent client using the launcher
above, e.g. for Claude Code:
  claude mcp add punakawan -- "$RunScript"
or point your client at the generated config: $GenericConfig

Write actions (Jira comments, transitions, subtasks, estimates) ask for one
inline human approval per run when the MCP client supports it. Otherwise use:
  punakawan approvals list
  punakawan approvals approve <id> --by <your-name>
"@
