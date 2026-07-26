package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/panel/runtime"
	"github.com/ygrip/punakawan/internal/panel/settings"
)

// panelSettingsResponse is the wire shape for GET/PATCH /api/v1/system/settings.
type panelSettingsResponse struct {
	MaxActiveRuntimes         int `json:"max_active_runtimes"`
	RuntimeIdleTimeoutSeconds int `json:"runtime_idle_timeout_seconds"`
}

// GetPanelSettingsHandler serves the current runtime-tunable panel settings.
// The live values come from the runtime manager (authoritative, since a prior
// PATCH applies to it immediately); root is where they persist.
func GetPanelSettingsHandler(root string, mgr *runtime.ProjectRuntimeManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := panelSettingsResponse{
			MaxActiveRuntimes:         settings.DefaultMaxActiveRuntimes,
			RuntimeIdleTimeoutSeconds: settings.DefaultRuntimeIdleTimeoutSeconds,
		}
		if mgr != nil {
			resp.MaxActiveRuntimes = mgr.MaxActive()
			resp.RuntimeIdleTimeoutSeconds = int(mgr.IdleTimeout() / time.Second)
		} else {
			s := settings.Load(root)
			resp = panelSettingsResponse{MaxActiveRuntimes: s.MaxActiveRuntimes, RuntimeIdleTimeoutSeconds: s.RuntimeIdleTimeoutSeconds}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// panelSettingsPatch is the PATCH body: each field optional so a caller can
// change one without restating the other.
type panelSettingsPatch struct {
	MaxActiveRuntimes         *int `json:"max_active_runtimes"`
	RuntimeIdleTimeoutSeconds *int `json:"runtime_idle_timeout_seconds"`
}

// UpdatePanelSettingsHandler persists a settings change under root and applies
// it to the live runtime manager at once (SetMaxActive can evict idle runtimes
// immediately, shutting down their dolt servers). Values are validated; a
// max_active_runtimes below 1 is rejected rather than silently clamped, so the
// user sees why.
func UpdatePanelSettingsHandler(root string, mgr *runtime.ProjectRuntimeManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var patch panelSettingsPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: invalid settings body: %w", err))
			return
		}
		if patch.MaxActiveRuntimes != nil && *patch.MaxActiveRuntimes < 1 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: max_active_runtimes must be >= 1"))
			return
		}
		if patch.RuntimeIdleTimeoutSeconds != nil && *patch.RuntimeIdleTimeoutSeconds < 1 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: runtime_idle_timeout_seconds must be >= 1"))
			return
		}

		// Start from the currently-persisted settings so an omitted field keeps
		// its existing value.
		current := settings.Load(root)
		if patch.MaxActiveRuntimes != nil {
			current.MaxActiveRuntimes = *patch.MaxActiveRuntimes
		}
		if patch.RuntimeIdleTimeoutSeconds != nil {
			current.RuntimeIdleTimeoutSeconds = *patch.RuntimeIdleTimeoutSeconds
		}
		if err := settings.Save(root, current); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if mgr != nil {
			mgr.SetMaxActive(current.MaxActiveRuntimes)
			mgr.SetIdleTimeout(time.Duration(current.RuntimeIdleTimeoutSeconds) * time.Second)
		}
		writeJSON(w, http.StatusOK, panelSettingsResponse{
			MaxActiveRuntimes:         current.MaxActiveRuntimes,
			RuntimeIdleTimeoutSeconds: current.RuntimeIdleTimeoutSeconds,
		})
	}
}
