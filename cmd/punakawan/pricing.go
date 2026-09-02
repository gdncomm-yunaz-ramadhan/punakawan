package main

import (
	"context"
	"log/slog"

	"github.com/ygrip/punakawan/internal/telemetry"
)

// primeModelRates installs current model prices over the process-wide
// pricing catalog, and says what it managed to load.
//
// This is a composition-root concern by design. Doing it inside
// telemetry.NewStore would make every Store constructor - and so every
// test that builds one - reach for the network and this machine's real
// config directory, so the fetch lives here, in the few processes that
// actually price usage: the hook that ingests it, and the long-lived
// servers that read it back.
//
// It never fails a caller. An unreachable feed leaves the compiled-in
// table in place, which prices every model punakawan already knew; only
// a model shipped since the last release goes unpriced, and that is
// reported as a delivery gap rather than swallowed.
func primeModelRates(ctx context.Context, logger *slog.Logger) telemetry.RatesStatus {
	status := (&telemetry.RatesFeed{}).Prime(ctx)
	if logger == nil {
		return status
	}
	attrs := []any{"origin", string(status.Origin), "rates", status.RateCount, "cache", status.CachePath}
	if status.Err != nil {
		logger.Info("model rates: using what is available offline", append(attrs, "error", status.Err)...)
		return status
	}
	logger.Debug("model rates loaded", attrs...)
	return status
}
