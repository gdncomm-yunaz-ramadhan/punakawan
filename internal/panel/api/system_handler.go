// Package api implements the Punakawan Panel's /api/v1 HTTP handlers, per
// punakawan-panel-implementation-plan.md §11. Handlers only translate
// between HTTP and internal/panel/contract's reader interfaces - no
// format-specific parsing lives here.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/registry"
)

// Config holds the server-wide facts SystemHandler reports, per §11.1's
// documented response fields.
type Config struct {
	PunakawanVersion string
	ReadOnly         bool
	BoundAddr        string
	StartedAt        time.Time
}

// SystemInfo is GET /api/v1/system's response shape.
type SystemInfo struct {
	PanelVersion         string    `json:"panel_version"`
	PunakawanVersion     string    `json:"punakawan_version"`
	ServerStartTime      time.Time `json:"server_start_time"`
	ReadOnly             bool      `json:"read_only"`
	BoundAddress         string    `json:"bound_address"`
	RegisteredWorkspaces int       `json:"registered_workspaces"`
	WatcherStatus        string    `json:"watcher_status"`
	FeatureFlags         []string  `json:"feature_flags"`
}

// writeJSON encodes v as the response body with a JSON content type. It
// is used by every handler in this package rather than each one repeating
// header-setting boilerplate.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a minimal {"error": "..."} body. §17.4 applies here
// too: err's message is server-generated text, not attacker-controlled
// HTML, so it is safe to return as-is inside a JSON string.
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// SystemHandler serves GET /api/v1/system.
//
// WatcherStatus and FeatureFlags are reported as fixed honest placeholders
// ("not_implemented" / empty) rather than fabricated data: the filesystem
// watcher (§19) and feature-flag mechanism don't exist yet - later phases
// wire real values in without changing this response's shape.
func SystemHandler(cfg Config, reg *registry.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count := 0
		if entries, err := reg.List(); err == nil {
			count = len(entries)
		}

		writeJSON(w, http.StatusOK, SystemInfo{
			PanelVersion:         panel.Version,
			PunakawanVersion:     cfg.PunakawanVersion,
			ServerStartTime:      cfg.StartedAt,
			ReadOnly:             cfg.ReadOnly,
			BoundAddress:         cfg.BoundAddr,
			RegisteredWorkspaces: count,
			WatcherStatus:        "not_implemented",
			FeatureFlags:         []string{},
		})
	}
}
