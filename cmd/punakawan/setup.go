package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var shell string
	var printScript bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Open a credential-configured subshell",
		Long:  "Prompts only for unset credentials, exports them, then opens a child shell where Punakawan commands inherit them. Credentials are never written to disk or Punakawan's database.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			script, err := setupScript(shell)
			if err != nil {
				return err
			}
			if printScript {
				_, err = fmt.Fprint(cmd.OutOrStdout(), script)
				return err
			}
			reportUsageTrackingHookSetup(cmd)
			return runSetupShell(shell, script, cmd)
		},
	}
	cmd.Flags().StringVar(&shell, "shell", defaultSetupShell(), "target shell: sh, powershell, or cmd")
	cmd.Flags().BoolVar(&printScript, "print", false, "print the sourceable setup script instead of executing it")
	return cmd
}

func defaultSetupShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "sh"
}

// reportUsageTrackingHookSetup ensures the current directory's
// .claude/settings.json declares punakawan's SubagentStop usage-tracking
// hook (see ensureUsageTrackingHook), printing one line to stderr reporting
// the outcome. It never fails setup over a hook-config hiccup - same
// never-fail philosophy as the hook's own CLI verb (cmd/punakawan
// hooks record-usage): a tracking convenience must never block getting a
// working credentialed shell.
func reportUsageTrackingHookSetup(cmd *cobra.Command) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "setup: skip usage-tracking hook: %v\n", err)
		return
	}
	changed, err := ensureUsageTrackingHook(cwd)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "setup: could not configure usage-tracking hook: %v\n", err)
		return
	}
	if changed {
		fmt.Fprintln(cmd.ErrOrStderr(), "setup: configured SubagentStop usage-tracking hook in .claude/settings.json")
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "setup: usage-tracking hook already configured")
	}
}

func runSetupShell(shell, script string, cmd *cobra.Command) error {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "sh", "bash", "zsh":
		target := os.Getenv("SHELL")
		if target == "" {
			target = "/bin/sh"
		}
		process := exec.Command(target, "-ic", script+"\nexec \""+target+"\" -i")
		process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
		return process.Run()
	case "powershell", "ps1":
		process := exec.Command("powershell", "-NoExit", "-Command", script)
		process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
		return process.Run()
	case "cmd", "bat":
		process := exec.Command("cmd", "/K", script)
		process.Stdin, process.Stdout, process.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
		return process.Run()
	default:
		return fmt.Errorf("unsupported setup shell %q; use sh, powershell, or cmd", shell)
	}
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
if [ -z "${GITHUB_TOKEN:-}" ] && [ -z "${GH_TOKEN:-}" ]; then
  printf 'GITHUB_TOKEN: ' >&2
  stty -echo
  IFS= read -r GITHUB_TOKEN
  stty echo
  printf '\n' >&2
  export GITHUB_TOKEN
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
if ([string]::IsNullOrWhiteSpace($env:GITHUB_TOKEN) -and [string]::IsNullOrWhiteSpace($env:GH_TOKEN)) {
  $secure = Read-Host 'GITHUB_TOKEN' -AsSecureString
  $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
  try { $env:GITHUB_TOKEN = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer) } finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
}
`, nil
	case "cmd", "bat":
		return `@if "%ATLASSIAN_HOST%"=="" set /p ATLASSIAN_HOST=ATLASSIAN_HOST (for example team.atlassian.net): 
@if "%ATLASSIAN_API_TOKEN%"=="" set /p ATLASSIAN_API_TOKEN=ATLASSIAN_API_TOKEN: 
@if not "%ATLASSIAN_API_TOKEN_SCOPED%"=="true" if not "%ATLASSIAN_API_TOKEN_SCOPED%"=="1" if "%ATLASSIAN_EMAIL%"=="" set /p ATLASSIAN_EMAIL=ATLASSIAN_EMAIL: 
@if "%GITHUB_TOKEN%"=="" if "%GH_TOKEN%"=="" set /p GITHUB_TOKEN=GITHUB_TOKEN: 
`, nil
	default:
		return "", fmt.Errorf("unsupported setup shell %q; use sh, powershell, or cmd", shell)
	}
}
