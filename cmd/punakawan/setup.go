package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var shell string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Print a sourceable Jira credential setup script",
		Long:  "Prints a script that prompts only for unset Jira environment variables. Run it in the current shell: eval \"$(punakawan setup)\" on POSIX shells, or punakawan setup --shell powershell | Invoke-Expression in PowerShell. Credentials are never written to Punakawan's database.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			script, err := setupScript(shell)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), script)
			return err
		},
	}
	cmd.Flags().StringVar(&shell, "shell", defaultSetupShell(), "target shell: sh, powershell, or cmd")
	return cmd
}

func defaultSetupShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "sh"
}

func setupScript(shell string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "sh", "bash", "zsh":
		return `if [ -z "${ATLASSIAN_HOST:-}" ]; then
  printf 'ATLASSIAN_HOST (for example team.atlassian.net): ' >&2
  IFS= read -r ATLASSIAN_HOST
  export ATLASSIAN_HOST
fi
if [ -z "${ATLASSIAN_API_TOKEN:-}" ]; then
  printf 'ATLASSIAN_API_TOKEN: ' >&2
  stty -echo
  IFS= read -r ATLASSIAN_API_TOKEN
  stty echo
  printf '\n' >&2
  export ATLASSIAN_API_TOKEN
fi
if [ "${ATLASSIAN_API_TOKEN_SCOPED:-}" != "true" ] && [ "${ATLASSIAN_API_TOKEN_SCOPED:-}" != "1" ] && [ -z "${ATLASSIAN_EMAIL:-}" ]; then
  printf 'ATLASSIAN_EMAIL: ' >&2
  IFS= read -r ATLASSIAN_EMAIL
  export ATLASSIAN_EMAIL
fi
`, nil
	case "powershell", "ps1":
		return `if ([string]::IsNullOrWhiteSpace($env:ATLASSIAN_HOST)) { $env:ATLASSIAN_HOST = Read-Host 'ATLASSIAN_HOST (for example team.atlassian.net)' }
if ([string]::IsNullOrWhiteSpace($env:ATLASSIAN_API_TOKEN)) {
  $secure = Read-Host 'ATLASSIAN_API_TOKEN' -AsSecureString
  $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
  try { $env:ATLASSIAN_API_TOKEN = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer) } finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
}
if ($env:ATLASSIAN_API_TOKEN_SCOPED -notin @('true', '1') -and [string]::IsNullOrWhiteSpace($env:ATLASSIAN_EMAIL)) { $env:ATLASSIAN_EMAIL = Read-Host 'ATLASSIAN_EMAIL' }
`, nil
	case "cmd", "bat":
		return `@if "%ATLASSIAN_HOST%"=="" set /p ATLASSIAN_HOST=ATLASSIAN_HOST (for example team.atlassian.net): 
@if "%ATLASSIAN_API_TOKEN%"=="" set /p ATLASSIAN_API_TOKEN=ATLASSIAN_API_TOKEN: 
@if not "%ATLASSIAN_API_TOKEN_SCOPED%"=="true" if not "%ATLASSIAN_API_TOKEN_SCOPED%"=="1" if "%ATLASSIAN_EMAIL%"=="" set /p ATLASSIAN_EMAIL=ATLASSIAN_EMAIL: 
`, nil
	default:
		return "", fmt.Errorf("unsupported setup shell %q; use sh, powershell, or cmd", shell)
	}
}
