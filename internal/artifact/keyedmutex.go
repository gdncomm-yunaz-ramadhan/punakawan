package artifact

import "sync"

// keyedMutex serializes operations against the same string key (a review
// id, an artifact id) within one process, without serializing unrelated
// keys against each other. It exists because PlanStore/ReviewStore's own
// methods (PutRevisionRequest's exists-check-then-write, CreateVersion's
// latestVersion-then-write) are each individually safe for concurrent
// distinct keys but not for a caller's own multi-step
// read-check-write sequence spanning several store calls (e.g.
// SubmitHandler's "is this review still a draft, does its idempotency key
// already have a submission, if not create one and flip status" - three
// separate store calls that must not interleave with another goroutine's
// copy of the same sequence for the same review).
//
// This is a single-process, in-memory lock only - it does not (and, per
// §17's "local, single-user tool" scope, does not need to) coordinate
// across multiple panel processes sharing one workspace directory.
//
// Exported (KeyedMutex/NewKeyedMutex) so other artifact.Store
// implementations outside this package - e.g. internal/recipe's
// RecipeStore - can give their own CreateVersion/Current sequences the
// same per-artifact-id serialization PlanStore's LockArtifact provides,
// without duplicating this type.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewKeyedMutex() *KeyedMutex {
	return &KeyedMutex{locks: make(map[string]*sync.Mutex)}
}

// Lock blocks until key's lock is held and returns a func that releases
// it - intended to be used as `defer m.Lock(id)()`.
func (m *KeyedMutex) Lock(key string) func() {
	m.mu.Lock()
	l, ok := m.locks[key]
	if !ok {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	m.mu.Unlock()

	l.Lock()
	return l.Unlock
}
