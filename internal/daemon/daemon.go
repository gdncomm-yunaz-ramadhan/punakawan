package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygrip/punakawan/internal/storage"
)

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
	paths     Paths
	lock      *Lock
	db        *storage.DB
	transport *Transport
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

	d := &Daemon{paths: paths, lock: lock, db: db}
	transport, err := NewTransport(host, port, token, d.readyCheck)
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

// Serve blocks accepting connections until Shutdown is called.
func (d *Daemon) Serve() error {
	return d.transport.Serve()
}

// Shutdown gracefully stops the transport, closes storage, removes the
// published port file, and releases the singleton lock - in that order,
// so no client can observe a lock held with storage already closed
// underneath it.
func (d *Daemon) Shutdown(ctx context.Context) error {
	err := d.transport.Shutdown(ctx)
	if cerr := d.db.Close(); err == nil {
		err = cerr
	}
	os.Remove(d.paths.PortPath)
	if rerr := d.lock.Release(); err == nil {
		err = rerr
	}
	return err
}
