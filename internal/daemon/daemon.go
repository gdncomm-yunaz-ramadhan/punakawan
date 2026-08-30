package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/procreg"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/workspace"
)

// outboxWorkers is the default bounded worker pool size the daemon runs
// against the provider outbox.
const outboxWorkers = 2

// outboxIdle is how long an idle worker waits between empty claim attempts.
const outboxIdle = 2 * time.Second

// Paths bundles the on-disk locations Run needs, all colocated under
// the same storage.DataDir - one clear ownership tree per install.
type Paths struct {
	LockPath  string
	TokenPath string
	PortPath  string
	DBPath    string
}

// DefaultPaths resolves Paths from the platform-standard data directory
// (internal/storage.DataDir), creating nothing itself - Run's callees
// create each file lazily.
func DefaultPaths() (Paths, error) {
	dir, err := storage.DataDir()
	if err != nil {
		return Paths{}, err
	}
	dbPath, err := storage.DBPath()
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		LockPath:  filepath.Join(dir, "daemon.lock"),
		TokenPath: filepath.Join(dir, "daemon.token"),
		PortPath:  filepath.Join(dir, "daemon.port"),
		DBPath:    dbPath,
	}, nil
}

// Daemon owns the storage kernel and the authenticated loopback
// transport for exactly one Punakawan install. It is the only thing in
// the process tree allowed to call storage.Open against DBPath - every
// CLI, MCP, and Panel client must reach data through Client instead.
type Daemon struct {
	paths      Paths
	lock       *Lock
	db         *storage.DB
	transport  *Transport
	processes  *procreg.Registry
	reconciled procreg.Result
	adapters   *adapters.Registry
	pool       *providerwrite.Pool
}

// Run acquires the singleton lock, opens storage, binds the loopback
// transport, publishes the bound port for clients to discover, and
// serves until ctx is cancelled. It returns ErrAlreadyRunning (without
// having touched storage) if another live daemon already holds the
// lock - callers should treat that as success from the caller's
// perspective (a daemon is available), not a failure to report.
func Run(ctx context.Context, host, port string, paths Paths) (*Daemon, error) {
	lock, err := AcquireSingleton(paths.LockPath)
	if err != nil {
		return nil, err
	}

	if err := storage.CheckLocation(paths.DBPath); err != nil {
		lock.Release()
		return nil, err
	}
	db, err := storage.Open(ctx, paths.DBPath)
	if err != nil {
		lock.Release()
		return nil, err
	}

	token, err := LoadOrCreateToken(paths.TokenPath)
	if err != nil {
		db.Close()
		lock.Release()
		return nil, err
	}

	processes := procreg.New(db)
	reconciled, err := processes.Reconcile(ctx)
	if err != nil {
		db.Close()
		lock.Release()
		return nil, fmt.Errorf("daemon: reconcile owned processes: %w", err)
	}

	d := &Daemon{paths: paths, lock: lock, db: db, processes: processes, reconciled: reconciled}
	// The daemon is the only process allowed to storage.Open DBPath (see
	// Daemon's own doc comment), so it mints the one delivery.Store every
	// HTTP route in this package reads and writes through - no MCP-style
	// OpenDeliveryStore-per-call here, since there is nothing else for the
	// daemon to scope storage.Open against.
	deliveryStore := delivery.NewStore(db)
	// Best-effort startup janitor (PR1 §3.6): reconciles orphaned lane
	// worktrees left behind by an interrupted cleanup. Never blocks or
	// fails startup - deliveryStore.ReconcileWorktrees logs and skips
	// anything it cannot safely resolve.
	deliveryStore.ReconcileWorktrees(ctx)
	transport, err := NewTransport(host, port, token, d.readyCheck, deliveryStore)
	if err != nil {
		db.Close()
		lock.Release()
		return nil, err
	}
	d.transport = transport

	if err := os.WriteFile(paths.PortPath, []byte(transport.Addr()), 0o600); err != nil {
		transport.Shutdown(ctx)
		db.Close()
		lock.Release()
		return nil, fmt.Errorf("daemon: publish bound address: %w", err)
	}

	// The daemon spans every project on this machine, so its adapter
	// registry is built from host-level config only - there is no single
	// workspace root to merge per-repo overrides against the way
	// internal/app.Load does for a single-repo CLI invocation.
	global, err := workspace.LoadGlobalConfig()
	if err != nil {
		transport.Shutdown(ctx)
		db.Close()
		lock.Release()
		return nil, fmt.Errorf("daemon: load adapter config: %w", err)
	}
	specs := make(map[string]adapters.AdapterSpec, len(global.Adapters))
	for id, cfg := range global.Adapters {
		specs[id] = adapters.AdapterSpec{Command: cfg.Command, Args: cfg.Args, EnvPassthrough: cfg.EnvPassthrough}
	}
	registry := adapters.NewRegistry(specs)

	outboxStore := outbox.New(db)
	observer := jirahooks.NewWorklogSyncObserver(deliveryStore)
	pool := providerwrite.NewPool(outboxStore, registry, outboxWorkers, outboxIdle, observer)
	pool.Start(ctx)
	d.adapters = registry
	d.pool = pool

	return d, nil
}

// readyCheck backs /readyz: the daemon is ready once storage answers a
// trivial query, i.e. the writer connection is actually usable.
func (d *Daemon) readyCheck() error {
	return d.db.Reader().Ping()
}

// Addr returns the transport's bound loopback address.
func (d *Daemon) Addr() string { return d.transport.Addr() }

// DB exposes the daemon's storage handle for the in-process API layer
// (delivery.Store, taskstore.Store, etc.) that Serve wires up; it is not
// part of Client's contract - out-of-process callers only ever see the
// HTTP transport.
func (d *Daemon) DB() *storage.DB { return d.db }

// Processes is the durable process-ownership registry that worker/agent
// process launches register with before exposing their work as running.
func (d *Daemon) Processes() *procreg.Registry { return d.processes }

// Reconciled reports what Run's startup reconciliation did with any
// process-ownership records left behind by a previous daemon instance
// that did not shut down cleanly.
func (d *Daemon) Reconciled() procreg.Result { return d.reconciled }

// Serve blocks accepting connections until Shutdown is called.
func (d *Daemon) Serve() error {
	return d.transport.Serve()
}

// Shutdown gracefully stops the transport, stops the outbox worker pool
// (no further claims; any write already in flight observes ctx
// cancellation and is classified ambiguous, never silently dropped; any
// claim left unfinished simply expires its lease), closes the adapter
// registry, closes storage, removes the published port file, and releases
// the singleton lock - in that order, so no client can observe a lock held
// with storage already closed underneath it, and no adapter subprocess is
// killed while a worker might still be waiting on its response.
func (d *Daemon) Shutdown(ctx context.Context) error {
	err := d.transport.Shutdown(ctx)
	if perr := d.pool.Stop(ctx); err == nil {
		err = perr
	}
	if aerr := d.adapters.Close(ctx); err == nil {
		err = aerr
	}
	if cerr := d.db.Close(); err == nil {
		err = cerr
	}
	os.Remove(d.paths.PortPath)
	if rerr := d.lock.Release(); err == nil {
		err = rerr
	}
	return err
}
