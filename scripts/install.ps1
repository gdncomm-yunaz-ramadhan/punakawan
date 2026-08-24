#Requires -Version 5.1
<#
.SYNOPSIS
  Installs Punakawan from this checkout on Windows.

.PARAMETER DryRun
  Prints installation actions without changing the machine.
#>
[CmdletBinding()]
param(
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Split-Path -Parent $ScriptDir
$InstallDir = if ($env:PUNAKAWAN_INSTALL_DIR) {
    $env:PUNAKAWAN_INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA 'Programs\Punakawan'
}
$ConfigDir = if ($env:PUNAKAWAN_CONFIG_DIR) {
    $env:PUNAKAWAN_CONFIG_DIR
} else {
    Join-Path $env:APPDATA 'punakawan'
}

function Write-Step {
    param([string]$Message)
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Write-ManualInstall {
    param(
        [string]$Name,
        [string]$Command,
        [string]$Url
    )
    Write-Warning "Could not install $Name automatically."
    Write-Host "Manual install: $Command"
    Write-Host "Docs: $Url"
}

function Test-Command {
    param([string]$Name)
    return [bool](Get-Command $Name -ErrorAction SilentlyContinue)
}

function Update-ProcessPath {
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $env:Path = "$machinePath;$userPath"
}

function Install-WithWinget {
    param(
        [string]$CommandName,
        [string]$PackageId,
        [string]$ManualCommand,
        [string]$DocsUrl
    )

    if (Test-Command $CommandName) {
        Write-Step "$CommandName already installed"
        return $true
    }
    if (-not (Test-Command 'winget')) {
        Write-ManualInstall -Name $CommandName -Command $ManualCommand -Url $DocsUrl
        Write-Host 'winget: https://learn.microsoft.com/windows/package-manager/winget/'
        return $false
    }

    Write-Step "Installing $CommandName with winget"
    Write-Host "    winget install --exact --id $PackageId --accept-source-agreements --accept-package-agreements"
    if ($DryRun) {
        return $true
    }

    & winget install --exact --id $PackageId --accept-source-agreements --accept-package-agreements
    if ($LASTEXITCODE -eq 0) {
        Update-ProcessPath
        if (Test-Command $CommandName) {
            return $true
        }
    }

    Write-ManualInstall -Name $CommandName -Command $ManualCommand -Url $DocsUrl
    return $false
}

function Invoke-External {
    param(
        [string]$Display,
        [scriptblock]$Action
    )
    Write-Host "    $Display"
    if ($DryRun) {
        return
    }
    & $Action
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed ($LASTEXITCODE): $Display"
    }
}

function Find-McpClient {
    param(
        [string]$Name,
        [string]$Override
    )
    if ($Override) {
        if (Test-Path $Override) { return (Resolve-Path $Override).Path }
        return $null
    }
    $client = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($client) { return $client.Source }
    return $null
}

function Write-ManualMcpSetup {
    param(
        [string]$Client,
        [string]$PunakawanPath
    )
    if ($Client -eq 'Codex') {
        Write-Host "Manual setup: codex mcp add punakawan -- `"$PunakawanPath`" mcp serve"
    } else {
        Write-Host "Manual setup: claude mcp add punakawan --scope user -- `"$PunakawanPath`" mcp serve"
    }
}

function Register-McpClient {
    param(
        [string]$Label,
        [string]$ClientPath,
        [string[]]$RemoveArguments,
        [string[]]$AddArguments,
        [string]$PunakawanPath
    )
    if (-not $ClientPath) {
        Write-Warning "$Label not detected; skipping automatic registration."
        Write-ManualMcpSetup -Client $Label -PunakawanPath $PunakawanPath
        return
    }

    Write-Step "Registering Punakawan with $Label"
    Write-Host "    $ClientPath $($RemoveArguments -join ' ')"
    Write-Host "    $ClientPath $($AddArguments -join ' ')"
    if ($DryRun) { return }

    $registrationSucceeded = $false
    try {
        & $ClientPath @RemoveArguments *> $null
        & $ClientPath @AddArguments
        $registrationSucceeded = ($LASTEXITCODE -eq 0)
    } catch {
        Write-Warning "$Label registration command failed: $_"
    }

    if ($registrationSucceeded) {
        Write-Host "$Label configured. Restart $Label to load Punakawan."
    } else {
        Write-Warning "$Label registration failed. Punakawan remains installed."
        Write-ManualMcpSetup -Client $Label -PunakawanPath $PunakawanPath
    }
}

function Configure-McpClients {
    param([string]$PunakawanPath)

    $codex = Find-McpClient -Name 'codex' -Override $env:PUNAKAWAN_CODEX_BIN
    $claude = Find-McpClient -Name 'claude' -Override $env:PUNAKAWAN_CLAUDE_BIN

    Register-McpClient -Label 'Codex' -ClientPath $codex `
        -RemoveArguments @('mcp', 'remove', 'punakawan') `
        -AddArguments @('mcp', 'add', 'punakawan', '--', $PunakawanPath, 'mcp', 'serve') `
        -PunakawanPath $PunakawanPath
    Register-McpClient -Label 'Claude Code' -ClientPath $claude `
        -RemoveArguments @('mcp', 'remove', 'punakawan', '--scope', 'user') `
        -AddArguments @('mcp', 'add', 'punakawan', '--scope', 'user', '--', $PunakawanPath, 'mcp', 'serve') `
        -PunakawanPath $PunakawanPath

    $genericConfig = Join-Path $ConfigDir 'mcp-config.json'
    if ($DryRun) {
        Write-Step "Generic MCP config: $genericConfig"
        return
    }
    New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
    @{
        mcpServers = @{
            punakawan = @{
                command = $PunakawanPath
                args = @('mcp', 'serve')
            }
        }
    } | ConvertTo-Json -Depth 4 | Set-Content -Path $genericConfig -Encoding UTF8
    Write-Step "Wrote generic MCP config: $genericConfig"
}

$prerequisiteFailure = $false
if (-not (Install-WithWinget -CommandName 'go' -PackageId 'GoLang.Go' -ManualCommand 'winget install --exact --id GoLang.Go' -DocsUrl 'https://go.dev/doc/install')) {
    $prerequisiteFailure = $true
}
if (-not (Install-WithWinget -CommandName 'node' -PackageId 'OpenJS.NodeJS.LTS' -ManualCommand 'winget install --exact --id OpenJS.NodeJS.LTS' -DocsUrl 'https://nodejs.org/en/download')) {
    $prerequisiteFailure = $true
}

if (-not (Test-Command 'pnpm')) {
    if (-not (Test-Command 'npm') -and -not $DryRun) {
        Write-ManualInstall -Name 'pnpm' -Command 'npm install --global pnpm@11.15.1' -Url 'https://pnpm.io/installation'
        $prerequisiteFailure = $true
    } else {
        Write-Step 'Installing pnpm with npm'
        try {
            Invoke-External -Display 'npm install --global pnpm@11.15.1' -Action { npm install --global pnpm@11.15.1 }
            if (-not $DryRun) {
                Update-ProcessPath
                if (-not (Test-Command 'pnpm')) {
                    throw 'npm completed but pnpm is still unavailable on PATH'
                }
            }
        } catch {
            Write-ManualInstall -Name 'pnpm' -Command 'npm install --global pnpm@11.15.1' -Url 'https://pnpm.io/installation'
            $prerequisiteFailure = $true
        }
    }
} else {
    Write-Step 'pnpm already installed'
}

if ($prerequisiteFailure) {
    throw 'One or more prerequisites could not be installed. Complete the manual steps above, then rerun this installer.'
}

Write-Step 'Building Punakawan with embedded panel assets'
Push-Location $RepoRoot
try {
    Invoke-External -Display 'go mod download' -Action { go mod download }
    Invoke-External -Display 'pnpm install --frozen-lockfile' -Action { pnpm install --frozen-lockfile }
    Invoke-External -Display 'pnpm --filter @punakawan/panel build' -Action { pnpm --filter '@punakawan/panel' build }

    Write-Step 'Installing punakawan and punakawand'
    if ($DryRun) {
        Write-Host "    New-Item -ItemType Directory -Force -Path `"$InstallDir`""
    } else {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    }

    $previousGoBin = $env:GOBIN
    try {
        $env:GOBIN = $InstallDir
        Invoke-External -Display 'go install ./cmd/punakawan ./cmd/punakawand' -Action { go install ./cmd/punakawan ./cmd/punakawand }
    } finally {
        $env:GOBIN = $previousGoBin
    }
} finally {
    Pop-Location
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$userPathParts = @($userPath -split ';' | Where-Object { $_ })
if ($userPathParts -notcontains $InstallDir) {
    if ($DryRun) {
        Write-Host "    Add `"$InstallDir`" to user PATH"
    } else {
        $newUserPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
        $env:Path = "$env:Path;$InstallDir"
        Write-Step "Added $InstallDir to user PATH; new shells inherit it"
    }
}

Write-Step 'Verifying installation'
$punakawan = Join-Path $InstallDir 'punakawan.exe'
$punakawand = Join-Path $InstallDir 'punakawand.exe'
if ($DryRun) {
    Write-Host "    $punakawan --help"
} else {
    if (-not (Test-Path $punakawan)) { throw "Install did not produce $punakawan" }
    if (-not (Test-Path $punakawand)) { throw "Install did not produce $punakawand" }
    & $punakawan --help | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'punakawan --help failed' }
}

Write-Step 'Auto-configuring detected MCP clients'
Configure-McpClients -PunakawanPath $punakawan

Write-Host @"

==> Done.
Binary directory: $InstallDir
Generic MCP config: $ConfigDir\mcp-config.json
Panel: punakawan panel --workspace C:\absolute\path\to\project
MCP: punakawan mcp serve
"@
