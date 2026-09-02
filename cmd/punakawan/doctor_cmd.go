package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/mcpserver"
	"github.com/ygrip/punakawan/internal/panel/assets"
	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/telemetry"
	"github.com/ygrip/punakawan/internal/telemetry/clienthooks"
	"github.com/ygrip/punakawan/internal/workspace"
)

// doctorCheckTimeout bounds every individual live check (opening the
// storage kernel, spawning an adapter for a handshake, an authenticated
// provider read, ensuring the daemon is up) so one unreachable dependency
// cannot hang the whole report.
const doctorCheckTimeout = 10 * time.Second

// doctorStatusOK, doctorStatusMissing are the two check-independent status
// strings every field below can carry; anything else is a short,
// credential-value-free failure reason (never the secret itself - see
// resolveCredential/redaction tests).
const (
	doctorStatusOK      = "ok"
	doctorStatusMissing = "missing"
)

// doctorOK is the report shape for a single boolean-ish check that also
// wants to explain a failure.
type doctorOK struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// doctorAdapterReport is one adapter's four independent checks, per the
// exact JSON shape `doctor --json` promises: an adapter can be configured
// but untrusted (entrypoint failure), configured but unreachable
// (handshake failure), configured and reachable but missing a credential,
// or fully configured yet unable to reach the provider (connectivity
// failure) - collapsing these into one boolean would hide which of those
// four very different problems an operator needs to fix.
type doctorAdapterReport struct {
	Entrypoint   string `json:"entrypoint"`
	Handshake    string `json:"handshake"`
	Credentials  string `json:"credentials"`
	Connectivity string `json:"connectivity"`
}

// doctorReport is the exact `doctor --json` output shape.
type doctorReport struct {
	Storage         doctorOK                       `json:"storage"`
	Daemon          doctorOK                       `json:"daemon"`
	Adapters        map[string]doctorAdapterReport `json:"adapters"`
	Telemetry       map[string]string              `json:"telemetry"`
	PanelAssets     doctorOK                       `json:"panel_assets"`
	WorkflowStorage doctorOK                       `json:"workflow_storage"`
	// Pricing reports whether recent recorded usage could actually be
	// priced. The hook checks above only prove events arrive; they said
	// "complete" throughout the entire period in which every snapshot was
	// priced unknown because the catalog did not know the model.
	Pricing doctorOK `json:"pricing"`
}

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verify storage, the daemon, adapters, telemetry hooks, and panel assets end to end",
		Long: "Runs the real checks a working install depends on: the storage kernel opens and migrates, " +
			"the daemon can be reached, each configured adapter's entrypoint is trusted and its process " +
			"completes an initialize handshake, its required credentials are present and pass a live " +
			"authenticated read, the embedded panel assets are present, workflow storage is durable, and " +
			"each supported lifecycle-hook client's telemetry actually reaches the spool and database. " +
			"Every credential is checked only for presence and validity; its value never appears in this " +
			"command's output.",
		RunE: func(cmd *cobra.Command, args []string) error {
			report := runDoctor(cmd.Context())
			if asJSON {
				encoded, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return fmt.Errorf("doctor: encode report: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			} else {
				printDoctorReport(cmd.OutOrStdout(), report)
			}
			if !report.allOK() {
				return fmt.Errorf("doctor: one or more checks failed")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as machine-readable JSON")
	return cmd
}

// allOK reports whether every check in the report passed - required
// hook telemetry passes at "complete", "incomplete" is treated as a
// warning-only state (the hook is installed; only its live verification is
// unconfirmed), and "missing" fails, matching "only complete supports the
// guaranteed-telemetry claim" without making an otherwise-healthy install
// permanently fail doctor merely because trust for a hook has not yet been
// granted by the client.
func (r doctorReport) allOK() bool {
	if !r.Storage.OK || !r.Daemon.OK || !r.PanelAssets.OK || !r.WorkflowStorage.OK || !r.Pricing.OK {
		return false
	}
	for _, a := range r.Adapters {
		if a.Entrypoint != doctorStatusOK || a.Handshake != doctorStatusOK ||
			a.Credentials != doctorStatusOK || a.Connectivity != doctorStatusOK {
			return false
		}
	}
	for _, status := range r.Telemetry {
		if status == doctorStatusMissing {
			return false
		}
	}
	return true
}

func printDoctorReport(out interface{ Write([]byte) (int, error) }, r doctorReport) {
	line := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	statusWord := func(ok bool) string {
		if ok {
			return "OK"
		}
		return "FAIL"
	}
	line("storage          %-4s %s", statusWord(r.Storage.OK), r.Storage.Detail)
	line("daemon           %-4s %s", statusWord(r.Daemon.OK), r.Daemon.Detail)
	for _, id := range []string{"atlassian", "github"} {
		a, ok := r.Adapters[id]
		if !ok {
			continue
		}
		line("adapter %-10s entrypoint=%s handshake=%s credentials=%s connectivity=%s",
			id, a.Entrypoint, a.Handshake, a.Credentials, a.Connectivity)
	}
	for _, id := range []string{clienthooks.ClientKindCodex, clienthooks.ClientKindClaudeCode} {
		if status, ok := r.Telemetry[id]; ok {
			line("telemetry %-12s %s", id, status)
		}
	}
	line("pricing          %-4s %s", statusWord(r.Pricing.OK), r.Pricing.Detail)
	line("panel_assets     %-4s %s", statusWord(r.PanelAssets.OK), r.PanelAssets.Detail)
	line("workflow_storage %-4s %s", statusWord(r.WorkflowStorage.OK), r.WorkflowStorage.Detail)
}

// runDoctor performs every check and assembles the report. It never
// returns an error itself - an individual check's own failure to run (a
// timeout, a missing binary, an unreachable host) is recorded as that
// check's own failing status, not surfaced as a doctor-level error, so one
// broken subsystem never prevents reporting on every other one.
func runDoctor(ctx context.Context) doctorReport {
	report := doctorReport{
		Adapters:  map[string]doctorAdapterReport{},
		Telemetry: map[string]string{},
	}

	report.Storage = checkStorage(ctx)
	report.Daemon = checkDaemon(ctx)
	report.PanelAssets = checkPanelAssets()

	ws, wsErr := workspace.DiscoverOrEphemeral(currentDirOrEmpty())
	if wsErr != nil {
		detail := fmt.Sprintf("resolve workspace: %v", wsErr)
		report.Adapters["atlassian"] = doctorAdapterReport{Entrypoint: detail, Handshake: detail, Credentials: detail, Connectivity: detail}
		report.Adapters["github"] = doctorAdapterReport{Entrypoint: detail, Handshake: detail, Credentials: detail, Connectivity: detail}
		report.WorkflowStorage = doctorOK{OK: false, Detail: detail}
	} else {
		defer func() {
			if ws.Ephemeral {
				os.RemoveAll(ws.Root)
			}
		}()
		report.WorkflowStorage = checkWorkflowStorage(ws)

		global, err := workspace.LoadGlobalConfig()
		if err != nil {
			detail := fmt.Sprintf("load global adapter config: %v", err)
			report.Adapters["atlassian"] = doctorAdapterReport{Entrypoint: detail, Handshake: detail, Credentials: detail, Connectivity: detail}
			report.Adapters["github"] = doctorAdapterReport{Entrypoint: detail, Handshake: detail, Credentials: detail, Connectivity: detail}
		} else {
			merged := ws.MergeAdapters(global)
			report.Adapters["atlassian"] = checkAdapter(ctx, "atlassian", merged["atlassian"], ws.Root, atlassianCredentialCheck)
			report.Adapters["github"] = checkAdapter(ctx, "github", merged["github"], ws.Root, githubCredentialCheck)
		}
	}

	report.Telemetry[clienthooks.ClientKindCodex] = checkHookTelemetry(ctx, clienthooks.ClientKindCodex)
	report.Telemetry[clienthooks.ClientKindClaudeCode] = checkHookTelemetry(ctx, clienthooks.ClientKindClaudeCode)
	report.Pricing = checkPricing(ctx)

	return report
}

// checkPricing reports unpriceable recorded usage and an undrained spool -
// the two ways an installed, apparently healthy telemetry pipeline still
// produces a delivery whose cost is unknown.
func checkPricing(ctx context.Context) doctorOK {
	path, err := storage.DBPath()
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	db, err := storage.Open(checkCtx, path)
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	defer db.Close()

	models, err := telemetry.NewStore(db).UnresolvedModels(checkCtx, 200)
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}

	var problems []string
	if len(models) > 0 {
		problems = append(problems, "no catalog price for "+strings.Join(models, ", "))
	}
	if dataDir, err := storage.DataDir(); err == nil {
		if pending, err := telemetry.PendingSpoolFiles(dataDir); err == nil && len(pending) > 0 {
			problems = append(problems, fmt.Sprintf("%d usage event(s) still spooled and unapplied", len(pending)))
		}
	}
	if len(problems) > 0 {
		return doctorOK{OK: false, Detail: strings.Join(problems, "; ")}
	}
	return doctorOK{OK: true, Detail: "recent usage is fully priced"}
}

func currentDirOrEmpty() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

// checkStorage verifies the shared storage kernel actually opens and
// migrates at its resolved path - the same path and Open call every other
// entrypoint (OpenStorage) uses, so a passing check here means every
// other command backed by the kernel will also work.
func checkStorage(ctx context.Context) doctorOK {
	path, err := storage.DBPath()
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	if err := storage.CheckLocation(path); err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	db, err := storage.Open(checkCtx, path)
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	defer db.Close()
	return doctorOK{OK: true, Detail: path}
}

// checkDaemon ensures the singleton daemon is running (starting it if
// needed, exactly as `punakawan daemon start` does) and reachable over its
// authenticated loopback transport.
func checkDaemon(ctx context.Context) doctorOK {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	client, err := daemon.EnsureRunning(checkCtx, paths)
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	if err := client.Healthy(checkCtx); err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	return doctorOK{OK: true}
}

// checkPanelAssets confirms the embedded panel bundle actually has content
// - the placeholder committed to a fresh checkout still counts, since the
// point of this check is "the binary has something to serve", not "the
// real frontend build has run".
func checkPanelAssets() doctorOK {
	data, err := assets.Dist.ReadFile(assets.DistDir + "/index.html")
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	if len(data) == 0 {
		return doctorOK{OK: false, Detail: "embedded dist/index.html is empty"}
	}
	return doctorOK{OK: true}
}

// checkWorkflowStorage confirms workflow definitions/state persist to a
// directory that survives this workspace being closed - ws.Root itself for
// a real project, or the process-wide data directory for an ephemeral
// (no-project) one; see Workspace.WorkflowRoot.
func checkWorkflowStorage(ws *workspace.Workspace) doctorOK {
	root, err := ws.WorkflowRoot()
	if err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	dir := filepath.Join(root, ".punakawan", "workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return doctorOK{OK: false, Detail: err.Error()}
	}
	return doctorOK{OK: true, Detail: root}
}

// credentialCheck resolves and validates one adapter's provider
// credentials. env is the resolved credential set (process environment,
// falling back to the durable global credential file - see
// resolveCredential); it returns a short, value-free status for
// "credentials" (non-empty and internally consistent) and, only when that
// passes, performs the live authenticated read that becomes
// "connectivity".
type credentialCheck func(ctx context.Context, env map[string]string) (credentials, connectivity string)

// checkAdapter runs one adapter's four independent checks.
func checkAdapter(ctx context.Context, adapterID string, cfg workspace.AdapterConfig, workspaceRoot string, check credentialCheck) doctorAdapterReport {
	report := doctorAdapterReport{}

	if strings.TrimSpace(cfg.Command) == "" {
		report.Entrypoint = doctorStatusMissing
		report.Handshake = doctorStatusMissing
	} else {
		trustPath, err := storage.AdapterTrustFilePath()
		if err != nil {
			report.Entrypoint = err.Error()
		} else {
			trust, err := adapters.LoadTrustFile(trustPath)
			if err != nil {
				report.Entrypoint = err.Error()
			} else if err := adapters.RequireTrustedIfRepositoryLocal(cfg.Command, workspaceRoot, trust); err != nil {
				report.Entrypoint = err.Error()
			} else if missing := missingEntrypointFile(cfg); missing != "" {
				report.Entrypoint = fmt.Sprintf("entrypoint file not found: %s", missing)
			} else {
				report.Entrypoint = doctorStatusOK
			}
		}

		if report.Entrypoint == doctorStatusOK {
			report.Handshake = checkAdapterHandshake(ctx, adapterID, cfg)
		} else {
			report.Handshake = doctorStatusMissing
		}
	}

	env := resolveCredentialEnv(adapterProvider(adapterID), cfg.EnvPassthrough)
	credentials, connectivity := check(ctx, env)
	report.Credentials = credentials
	report.Connectivity = connectivity
	return report
}

// missingEntrypointFile returns the first declared entrypoint-looking
// argument (an absolute or relative path, as opposed to a bare flag) that
// does not exist on disk, or "" if every such argument resolves.
func missingEntrypointFile(cfg workspace.AdapterConfig) string {
	for _, arg := range cfg.Args {
		if !strings.ContainsAny(arg, "/\\") {
			continue
		}
		if _, err := os.Stat(arg); err != nil {
			return arg
		}
	}
	return ""
}

// checkAdapterHandshake spawns adapterID's configured process and
// completes the same capabilities+initialize handshake the real
// AdapterRegistry performs on first use, then shuts it down.
func checkAdapterHandshake(ctx context.Context, adapterID string, cfg workspace.AdapterConfig) string {
	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()

	registry := adapters.NewRegistry(map[string]adapters.AdapterSpec{
		adapterID: {Command: cfg.Command, Args: cfg.Args, EnvPassthrough: cfg.EnvPassthrough},
	})
	defer registry.Close(checkCtx)

	if _, err := registry.Gate(checkCtx, adapterID); err != nil {
		return err.Error()
	}
	return doctorStatusOK
}

// resolveCredentialEnv resolves each named variable from process environment,
// then the default organisation in the durable credential store, then the
// legacy global .env file. That order matches an operator's explicit process
// override while making `punakawan setup github` and `punakawan doctor` use
// the same source without copying tokens into two files.
func resolveCredentialEnv(provider providercreds.Provider, names []string) map[string]string {
	fileValues := readGlobalEnvFileBestEffort()
	storeValues := readDefaultProviderEnvBestEffort(provider)
	out := make(map[string]string, len(names))
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			out[name] = v
			continue
		}
		if v, ok := storeValues[name]; ok && v != "" {
			out[name] = v
			continue
		}
		if v, ok := fileValues[name]; ok && v != "" {
			out[name] = v
		}
	}
	return out
}

func adapterProvider(adapterID string) providercreds.Provider {
	provider, err := providercreds.ProviderForAdapterProgram(adapterID)
	if err != nil {
		return ""
	}
	return provider
}

func readDefaultProviderEnvBestEffort(provider providercreds.Provider) map[string]string {
	if provider == "" {
		return nil
	}
	path, err := workspace.GlobalCredentialsPath()
	if err != nil {
		return nil
	}
	org, err := providercreds.Open(path).Get(provider, "")
	if err != nil {
		return nil
	}
	values := make(map[string]string)
	for _, entry := range org.Env() {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			values[name] = value
		}
	}
	return values
}

func readGlobalEnvFileBestEffort() map[string]string {
	path, err := workspace.GlobalEnvPath()
	if err != nil {
		return nil
	}
	values, err := parseEnvFile(path)
	if err != nil {
		return nil
	}
	return values
}

// parseEnvFile reads a simple KEY=value-per-line credential file (the
// shape the installers and `setup` write): blank lines and lines starting
// with "#" are skipped, and a value may be shell-quoted the way `printf
// '%q'` (bash) or a literal string (PowerShell) would write it - quotes
// are stripped, nothing else is unescaped, since every value this file
// ever holds is a single token (a token or a hostname), never one
// containing the specific backslash sequences %q can emit.
func parseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		out[key] = value
	}
	return out, nil
}

// atlassianScoped reports whether env names a scoped (service-account)
// Atlassian token, mirroring packages/adapter-atlassian/src/restClient.ts's
// loadConfigFromEnv: explicit ATLASSIAN_API_TOKEN_SCOPED wins, otherwise a
// token is scoped exactly when no ATLASSIAN_EMAIL is configured.
func atlassianScoped(env map[string]string) bool {
	if v, ok := env["ATLASSIAN_API_TOKEN_SCOPED"]; ok && v != "" {
		scoped, err := strconv.ParseBool(v)
		if err == nil {
			return scoped
		}
	}
	return strings.TrimSpace(env["ATLASSIAN_EMAIL"]) == ""
}

// atlassianCredentialCheck validates ATLASSIAN_HOST/ATLASSIAN_API_TOKEN
// (and ATLASSIAN_EMAIL unless the token is scoped) are present, then
// performs an authenticated site/user metadata read - never a Jira issue
// lookup, which needs a key this check has no reason to assume exists.
func atlassianCredentialCheck(ctx context.Context, env map[string]string) (credentials, connectivity string) {
	host := strings.TrimSpace(env["ATLASSIAN_HOST"])
	token := strings.TrimSpace(env["ATLASSIAN_API_TOKEN"])
	scoped := atlassianScoped(env)
	email := strings.TrimSpace(env["ATLASSIAN_EMAIL"])

	var missing []string
	if host == "" {
		missing = append(missing, "ATLASSIAN_HOST")
	}
	if token == "" {
		missing = append(missing, "ATLASSIAN_API_TOKEN")
	}
	if !scoped && email == "" {
		missing = append(missing, "ATLASSIAN_EMAIL")
	}
	if len(missing) > 0 {
		return doctorStatusMissing + ": " + strings.Join(missing, ", "), doctorStatusMissing
	}

	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	if err := checkAtlassianConnectivity(checkCtx, host, token, email, scoped); err != nil {
		return doctorStatusOK, sanitizeConnectivityError(err)
	}
	return doctorStatusOK, doctorStatusOK
}

// githubCredentialCheck validates GITHUB_TOKEN (or GH_TOKEN) is present,
// then performs an authenticated GET /user read.
func githubCredentialCheck(ctx context.Context, env map[string]string) (credentials, connectivity string) {
	token := strings.TrimSpace(env["GITHUB_TOKEN"])
	if token == "" {
		token = strings.TrimSpace(env["GH_TOKEN"])
	}
	if token == "" {
		return doctorStatusMissing + ": GITHUB_TOKEN", doctorStatusMissing
	}

	apiBaseURL := strings.TrimSpace(env["GITHUB_API_URL"])
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com"
	}

	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	if err := checkGitHubConnectivity(checkCtx, token, apiBaseURL); err != nil {
		return doctorStatusOK, sanitizeConnectivityError(err)
	}
	return doctorStatusOK, doctorStatusOK
}

// sanitizeConnectivityError reduces a live-call failure to a short,
// credential-value-free reason: net/http's own errors never include
// header values, but this still avoids ever echoing raw response bodies
// (which, for an auth failure, sometimes echo the request back) verbatim.
func sanitizeConnectivityError(err error) string {
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return "error: " + msg
}

// checkAtlassianConnectivity performs the same authenticated request a
// real Punakawan Jira read would, using the exact Basic/Bearer choice and
// scoped-token cloud-gateway resolution
// packages/adapter-atlassian/src/restClient.ts implements, without
// spawning the adapter process itself - a plain, direct HTTP round trip is
// enough to prove the credential is valid and reachable.
func checkAtlassianConnectivity(ctx context.Context, host, token, email string, scoped bool) error {
	_, err := atlassianAccount(ctx, host, token, email, scoped)
	return err
}

// atlassianAccount performs that same read and returns the account it
// answers for, so a stored credential can say whose it is without anyone
// typing it twice.
func atlassianAccount(ctx context.Context, host, token, email string, scoped bool) (string, error) {
	authHeader := "Bearer " + token
	if email != "" {
		authHeader = "Basic " + basicAuthValue(email, token)
	}

	url := "https://" + host + "/rest/api/3/myself"
	if scoped {
		cloudID, err := resolveAtlassianCloudID(ctx, host)
		if err != nil {
			return "", err
		}
		url = "https://api.atlassian.com/ex/jira/" + cloudID + "/rest/api/3/myself"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("atlassian site/user metadata request returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		EmailAddress string `json:"emailAddress"`
		DisplayName  string `json:"displayName"`
	}
	// See gitHubAccount: an unparseable body leaves the account unknown,
	// never the credential unverified.
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", nil
	}
	if account := strings.TrimSpace(body.EmailAddress); account != "" {
		return account, nil
	}
	return strings.TrimSpace(body.DisplayName), nil
}

func resolveAtlassianCloudID(ctx context.Context, host string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/_edge/tenant_info", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("resolve atlassian cloud id: HTTP %d", resp.StatusCode)
	}
	var body struct {
		CloudID string `json:"cloudId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("resolve atlassian cloud id: decode response: %w", err)
	}
	if body.CloudID == "" {
		return "", fmt.Errorf("resolve atlassian cloud id: response named no cloudId")
	}
	return body.CloudID, nil
}

func basicAuthValue(email, token string) string {
	return base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
}

// checkGitHubConnectivity performs an authenticated GET /user request.
func checkGitHubConnectivity(ctx context.Context, token, apiBaseURL string) error {
	_, err := gitHubAccount(ctx, token, apiBaseURL)
	return err
}

// gitHubAccount performs that same request and returns the account it
// answers for. The token is what identifies a GitHub account, so the
// account is read back from the provider rather than asked for - an
// answer nobody can mistype, and the only one that is actually true of
// the credential being stored.
func gitHubAccount(ctx context.Context, token, apiBaseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiBaseURL, "/")+"/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github GET /user returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		Login string `json:"login"`
	}
	// A body this host cannot parse does not make a verified credential
	// unverified: the status code already proved it works, so the account
	// is simply unknown.
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", nil
	}
	return strings.TrimSpace(body.Login), nil
}

// checkHookTelemetry reports one of complete/incomplete/missing for
// client's lifecycle-hook telemetry: missing when no hook configuration
// names this installed binary at all, complete when a live probe event
// makes it all the way through spooling and ingestion into a real
// telemetry session, incomplete when the hook is configured but that probe
// could not be confirmed (the client-side trust/load half of hook
// execution is outside this process's visibility - see setup_hooks.go).
func checkHookTelemetry(ctx context.Context, client string) string {
	binaryPath, err := resolvePanelServiceBinary()
	if err != nil {
		return doctorStatusMissing
	}
	if !hookConfigInstalled(client, binaryPath) {
		return doctorStatusMissing
	}

	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	if err := runHookProbe(checkCtx, client); err != nil {
		return "incomplete"
	}
	return "complete"
}

// hookConfigInstalled reports whether client's hook configuration
// (~/.codex/hooks.json for codex, ~/.claude/settings.json for
// claude-code) already declares every event this package installs,
// naming binaryPath.
func hookConfigInstalled(client, binaryPath string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	var path string
	var events []hookEventSpec
	switch client {
	case clienthooks.ClientKindCodex:
		path = filepath.Join(home, ".codex", "hooks.json")
		events = codexHookEvents
	case clienthooks.ClientKindClaudeCode:
		path = filepath.Join(home, ".claude", "settings.json")
		events = claudeCodeHookEvents
	default:
		return false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	for _, spec := range events {
		if !hasIngestHookEntry(hooks, spec.EventName, binaryPath, client) {
			return false
		}
	}
	return true
}

// runHookProbe exercises the exact hook -> spool -> ingest path a real
// client lifecycle event takes: a synthetic SessionStart payload naming a
// throwaway workspace, run through the same ingestHookEvent every `hooks
// ingest` invocation uses, then confirmed durably recorded by reading it
// back from the storage kernel by its external session id.
func runHookProbe(ctx context.Context, client string) error {
	tmp, err := os.MkdirTemp("", "punakawan-hook-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Mkdir(filepath.Join(tmp, ".git"), 0o755); err != nil {
		return err
	}

	externalSessionID := "doctor-probe-" + telemetry.NewEventID()
	marker := mcpserver.SessionMarker{
		SessionID: "probe", ExecutionID: "probe",
		OrchestrationID: "punakawan-doctor-probe-" + externalSessionID,
	}
	markerDir := filepath.Join(tmp, mcpserver.SessionMarkerDir)
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		return err
	}
	markerData, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(markerDir, mcpserver.SessionMarkerFile), markerData, 0o644); err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]string{"session_id": externalSessionID, "cwd": tmp})
	if err != nil {
		return err
	}
	var stdout, stderr strings.Builder
	ingestHookEvent(ctx, client, "SessionStart", strings.NewReader(string(payload)), &stdout, &stderr)

	a, err := app.Load(tmp)
	if err != nil {
		return fmt.Errorf("probe: %w (hook output: %s)", err, stderr.String())
	}
	defer a.Close()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		return fmt.Errorf("probe: %w (hook output: %s)", err, stderr.String())
	}
	store := telemetry.NewStore(db)
	if _, err := store.GetSessionByExternalID(ctx, client, externalSessionID); err != nil {
		return fmt.Errorf("probe session was not recorded: %w (hook output: %s)", err, stderr.String())
	}
	return nil
}
