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
# $ConfigDir is the exact directory internal/storage.DataDir resolves on
# Windows (${PUNAKAWAN_DATA_DIR} if set, else %AppData%\punakawan): every
# installed, machine-wide state - the storage kernel, the adapter trust
# file, the telemetry spool, and everything this installer writes below -
# lives under one directory so a relocated/overridden prefix cannot
# diverge between what the installer wrote and what the built binary
# resolves at runtime. PUNAKAWAN_CONFIG_DIR is accepted as a deprecated
# alias for one release.
$ConfigDir = if ($env:PUNAKAWAN_DATA_DIR) {
    $env:PUNAKAWAN_DATA_DIR
} elseif ($env:PUNAKAWAN_CONFIG_DIR) {
    $env:PUNAKAWAN_CONFIG_DIR
} else {
    Join-Path $env:APPDATA 'punakawan'
}
$AdaptersDir = Join-Path $ConfigDir 'adapters'

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

# Deploy-Adapter installs one @punakawan workspace package's built runtime
# files plus its production dependencies below "$AdaptersDir\$Slug" -
# never inside $RepoRoot - using pnpm's own workspace deploy support so
# relative workspace:* dependencies (adapter-sdk, schema-types) resolve to
# real copied files instead of symlinks back into the checkout. The new
# version deploys to its own versioned directory first; only once that
# succeeds is the stable "$AdaptersDir\$Slug" name repointed at it (a
# junction, which - unlike an NTFS symlink - a non-administrator account
# can create), and only then are older versioned directories removed.
function Deploy-Adapter {
    param(
        [string]$PackageName,
        [string]$Slug
    )

    $target = Join-Path $AdaptersDir $Slug
    if ($DryRun) {
        Write-Host "    pnpm --filter $PackageName deploy --prod --legacy $target.<version> (atomically replacing $target)"
        return
    }

    New-Item -ItemType Directory -Force -Path $AdaptersDir | Out-Null
    $stamp = Get-Date -Format 'yyyyMMddHHmmss'
    $versionedDir = "$target.$stamp"
    if (Test-Path $versionedDir) { Remove-Item -Recurse -Force $versionedDir }

    Push-Location $RepoRoot
    try {
        Invoke-External -Display "pnpm --filter $PackageName deploy --prod --legacy $versionedDir" -Action { pnpm --filter $PackageName deploy --prod --legacy $versionedDir }
    } finally {
        Pop-Location
    }

    if (Test-Path $target) {
        # DirectoryInfo.Delete() (no -Recurse) removes only the reparse
        # point itself, never descending into the versioned directory it
        # points at - Remove-Item -Recurse on a junction/symlink has
        # historically done the opposite on some PowerShell versions.
        (Get-Item $target -Force).Delete()
    }
    New-Item -ItemType Junction -Path $target -Target $versionedDir | Out-Null

    Get-ChildItem -Path $AdaptersDir -Directory -Filter "$Slug.*" -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -ne $versionedDir } |
        ForEach-Object { Remove-Item -Recurse -Force $_.FullName }
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

    # Always replace this adapter id's own block (never someone else's)
    # rather than skipping when it already exists, so a later install that
    # deploys a new adapter version actually updates the recorded
    # entrypoint path instead of leaving a stale one.
    $content = Get-Content -Raw -Path $ConfigPath
    $blockPattern = "(?m)^  $([regex]::Escape($AdapterId)):\s*(?:#.*)?$(?:\r?\n(?:    .*)?$)*"
    $content = [regex]::Replace($content, $blockPattern, '')
    Set-Content -Path $ConfigPath -Value $content.TrimEnd("`r", "`n") -Encoding UTF8

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
    Register-McpClient -Label 'Claude Code' -ClientPath $claude `
        -RemoveArguments @('mcp', 'remove', 'punakawan', '--scope', 'user') `
        -AddArguments (@('mcp', 'add', 'punakawan', '--scope', 'user', '--', $mcpCommand) + $mcpArguments) `
        -McpCommand $mcpCommand -McpArguments $mcpArguments

    # User-level hooks are installed regardless of which client binaries
    # were actually detected above: the hook config files
    # (~/.codex/hooks.json, ~/.claude/settings.json) are independent of
    # whether that client's CLI happens to be on PATH right now.
    Write-Step 'Configuring Codex and Claude Code lifecycle telemetry hooks'
    if ($DryRun) {
        Write-Host "    $PunakawanPath setup --hooks-only"
    } else {
        & $PunakawanPath setup --hooks-only
        if ($LASTEXITCODE -ne 0) {
            Write-Warning 'Could not configure lifecycle telemetry hooks; delivery usage tracking will be incomplete until this is retried (run `punakawan setup` or check `punakawan doctor`).'
        }
    }

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

# Stop-StaleDaemon stops any already-running punakawand. Without this, an
# already running daemon keeps serving the previous checkout's wire format
# indefinitely - go install replaces the file on disk, but a running
# process keeps executing the old image, and there is no version
# handshake between the two, so a client built from this install would
# otherwise silently talk to a stale daemon until someone happened to
# restart it by hand. It deliberately does not also start a new one here:
# every command that needs the daemon (panel, mcp serve, doctor, ...)
# already starts it on demand.
function Stop-StaleDaemon {
    param([string]$PunakawanPath)

    if ($DryRun) {
        Write-Host "    $PunakawanPath daemon stop"
        return
    }
    & $PunakawanPath daemon stop | Out-Null
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
    # Adapters (and the workspace packages they depend on) build in this
    # exact order before Deploy-Adapter packages them below, so it always
    # packages a freshly built dist\, never a stale one left over from an
    # earlier checkout.
    Invoke-External -Display 'pnpm --filter @punakawan/schema-types build' -Action { pnpm --filter @punakawan/schema-types build }
    Invoke-External -Display 'pnpm --filter @punakawan/adapter-sdk build' -Action { pnpm --filter @punakawan/adapter-sdk build }
    Invoke-External -Display 'pnpm --filter @punakawan/adapter-atlassian build' -Action { pnpm --filter @punakawan/adapter-atlassian build }
    Invoke-External -Display 'pnpm --filter @punakawan/github-adapter build' -Action { pnpm --filter @punakawan/github-adapter build }
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

Write-Step 'Stopping any already-running Punakawan daemon so it is not left serving a stale build'
Stop-StaleDaemon -PunakawanPath (Join-Path $InstallDir 'punakawan.exe')

$atlassianAdapter = Join-Path $AdaptersDir 'atlassian\dist\run.js'
$githubAdapter = Join-Path $AdaptersDir 'github\dist\run.js'
$globalConfig = Join-Path $ConfigDir 'config.yaml'
$globalEnvironment = Join-Path $ConfigDir '.env'
if ($DryRun) {
    Deploy-Adapter -PackageName '@punakawan/adapter-atlassian' -Slug 'atlassian'
    Deploy-Adapter -PackageName '@punakawan/github-adapter' -Slug 'github'
    Write-Step "Configuring global adapters: $globalConfig"
} else {
    Deploy-Adapter -PackageName '@punakawan/adapter-atlassian' -Slug 'atlassian'
    Deploy-Adapter -PackageName '@punakawan/github-adapter' -Slug 'github'
    if (-not (Test-Path $atlassianAdapter)) { throw "Deploy did not produce $atlassianAdapter" }
    if (-not (Test-Path $githubAdapter)) { throw "Deploy did not produce $githubAdapter" }

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
