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
$InstallDirOverridden = [bool]$env:PUNAKAWAN_INSTALL_DIR
$InstallDir = if ($InstallDirOverridden) {
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
        [string]$McpCommand,
        [string[]]$McpArguments
    )
    $quotedArguments = ($McpArguments | ForEach-Object { "`"$_`"" }) -join ' '
    $invocation = "`"$McpCommand`" $quotedArguments"
    if ($Client -eq 'Codex') {
        Write-Host "Manual setup: codex mcp add punakawan -- $invocation"
    } else {
        Write-Host "Manual setup: claude mcp add punakawan --scope user -- $invocation"
    }
}

function Register-McpClient {
    param(
        [string]$Label,
        [string]$ClientPath,
        [string[]]$RemoveArguments,
        [string[]]$AddArguments,
        [string]$McpCommand,
        [string[]]$McpArguments
    )
    if (-not $ClientPath) {
        Write-Warning "$Label not detected; skipping automatic registration."
        Write-ManualMcpSetup -Client $Label -McpCommand $McpCommand -McpArguments $McpArguments
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
        Write-ManualMcpSetup -Client $Label -McpCommand $McpCommand -McpArguments $McpArguments
    }
}

function Write-AdapterConfig {
    param(
        [string]$ConfigPath,
        [string]$AdapterId,
        [string]$EntryPoint,
        [string]$NodePath,
        [string[]]$EnvPassthrough
    )

    $directory = Split-Path -Parent $ConfigPath
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
    if (-not (Test-Path $ConfigPath)) {
        Set-Content -Path $ConfigPath -Value 'adapters:' -Encoding UTF8
    } elseif (-not ((Get-Content -Raw -Path $ConfigPath) -match '(?m)^adapters:\s*$')) {
        throw "Cannot safely add adapters to $ConfigPath; expected a block-style top-level adapters key."
    }

    if ((Get-Content -Raw -Path $ConfigPath) -match "(?m)^\s{2}$([regex]::Escape($AdapterId)):\s*(?:#.*)?$") {
        return
    }

    $lines = @("  $AdapterId:", "    command: $NodePath", '    args:', "      - $EntryPoint", '    env_passthrough:')
    foreach ($name in $EnvPassthrough) {
        $lines += "      - $name"
    }
    Add-Content -Path $ConfigPath -Value $lines -Encoding UTF8
}

function Write-EnvironmentFile {
    param([string]$EnvironmentPath)

    $directory = Split-Path -Parent $EnvironmentPath
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
    if (-not (Test-Path $EnvironmentPath)) {
        New-Item -ItemType File -Path $EnvironmentPath | Out-Null
    }
    $contents = Get-Content -Raw -Path $EnvironmentPath
    if ($contents -notmatch '(?m)^(GITHUB_TOKEN|GH_TOKEN)=') {
        if ($env:GITHUB_TOKEN) {
            Add-Content -Path $EnvironmentPath -Value "GITHUB_TOKEN=$($env:GITHUB_TOKEN)" -Encoding UTF8
        } elseif ($env:GH_TOKEN) {
            Add-Content -Path $EnvironmentPath -Value "GH_TOKEN=$($env:GH_TOKEN)" -Encoding UTF8
        }
    }
}

function Write-McpLauncher {
    param(
        [string]$PunakawanPath,
        [string]$EnvironmentPath,
        [string]$LauncherPath
    )

    $escapedPunakawan = $PunakawanPath.Replace("'", "''")
    $escapedEnvironment = $EnvironmentPath.Replace("'", "''")
    $script = @"
param([Parameter(ValueFromRemainingArguments = `$true)][string[]]`$McpArguments)
`$ErrorActionPreference = 'Stop'
if (Test-Path -LiteralPath '$escapedEnvironment') {
    Get-Content -LiteralPath '$escapedEnvironment' | ForEach-Object {
        if (`$_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
            [Environment]::SetEnvironmentVariable(`$matches[1], `$matches[2], 'Process')
        }
    }
}
& '$escapedPunakawan' @McpArguments
exit `$LASTEXITCODE
"@
    Set-Content -Path $LauncherPath -Value $script -Encoding UTF8
    return $LauncherPath
}

function Configure-McpClients {
    param(
        [string]$PunakawanPath,
        [string]$EnvironmentPath
    )

    $launcherPath = Join-Path $ConfigDir 'run-mcp.ps1'
    if (-not $DryRun) {
        Write-McpLauncher -PunakawanPath $PunakawanPath -EnvironmentPath $EnvironmentPath -LauncherPath $launcherPath | Out-Null
        Write-Step "Wrote MCP launcher: $launcherPath"
    }
    $mcpCommand = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-Path $mcpCommand)) { $mcpCommand = 'powershell.exe' }
    $mcpArguments = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $launcherPath, 'mcp', 'serve')

    $codex = Find-McpClient -Name 'codex' -Override $env:PUNAKAWAN_CODEX_BIN
    $claude = Find-McpClient -Name 'claude' -Override $env:PUNAKAWAN_CLAUDE_BIN

    Register-McpClient -Label 'Codex' -ClientPath $codex `
        -RemoveArguments @('mcp', 'remove', 'punakawan') `
        -AddArguments (@('mcp', 'add', 'punakawan', '--', $mcpCommand) + $mcpArguments) `
        -McpCommand $mcpCommand -McpArguments $mcpArguments
    if ($codex) {
        Write-Step 'Configuring Codex lifecycle telemetry hooks'
        if ($DryRun) {
            Write-Host "    $PunakawanPath hooks install-global"
        } else {
            & $PunakawanPath hooks install-global
            if ($LASTEXITCODE -ne 0) {
                Write-Warning 'Could not configure Codex lifecycle hooks; delivery usage tracking for Codex sessions will be incomplete until this is retried.'
            }
        }
    }
    Register-McpClient -Label 'Claude Code' -ClientPath $claude `
        -RemoveArguments @('mcp', 'remove', 'punakawan', '--scope', 'user') `
        -AddArguments (@('mcp', 'add', 'punakawan', '--scope', 'user', '--', $mcpCommand) + $mcpArguments) `
        -McpCommand $mcpCommand -McpArguments $mcpArguments

    $genericConfig = Join-Path $ConfigDir 'mcp-config.json'
    if ($DryRun) {
        Write-Step "Generic MCP config: $genericConfig"
        return
    }
    New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
    @{
        mcpServers = @{
            punakawan = @{
                command = $mcpCommand
                args = $mcpArguments
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
    Invoke-External -Display 'pnpm -r --if-present build' -Action { pnpm -r --if-present build }

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

$atlassianAdapter = Join-Path $RepoRoot 'packages\adapter-atlassian\dist\run.js'
$githubAdapter = Join-Path $RepoRoot 'packages\github-adapter\dist\run.js'
if (-not $DryRun) {
    if (-not (Test-Path $atlassianAdapter)) { throw "Build did not produce $atlassianAdapter" }
    if (-not (Test-Path $githubAdapter)) { throw "Build did not produce $githubAdapter" }
}
$globalConfig = Join-Path $ConfigDir 'config.yaml'
$globalEnvironment = Join-Path $ConfigDir '.env'
if ($DryRun) {
    Write-Step "Configuring global adapters: $globalConfig"
} else {
    $nodePath = (Get-Command node -CommandType Application).Source
    Write-AdapterConfig -ConfigPath $globalConfig -AdapterId 'atlassian' -EntryPoint $atlassianAdapter -NodePath $nodePath -EnvPassthrough @('ATLASSIAN_API_TOKEN', 'ATLASSIAN_API_TOKEN_SCOPED', 'ATLASSIAN_HOST', 'ATLASSIAN_EMAIL')
    Write-AdapterConfig -ConfigPath $globalConfig -AdapterId 'github' -EntryPoint $githubAdapter -NodePath $nodePath -EnvPassthrough @('GITHUB_TOKEN', 'GH_TOKEN', 'GITHUB_API_URL', 'GITHUB_GRAPHQL_URL')
    Write-EnvironmentFile -EnvironmentPath $globalEnvironment
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$userPathParts = @($userPath -split ';' | Where-Object { $_ })
if (-not $InstallDirOverridden -and $userPathParts -notcontains $InstallDir) {
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
Configure-McpClients -PunakawanPath $punakawan -EnvironmentPath $globalEnvironment

Write-Host @"

==> Done.
Binary directory: $InstallDir
Generic MCP config: $ConfigDir\mcp-config.json
Credentials: $globalEnvironment
Global adapters: $globalConfig
Panel: punakawan panel --workspace C:\absolute\path\to\project
MCP: punakawan mcp serve
"@
