// Package procident answers one question per OS: exactly when did the
// process currently running as this pid start? That value is stable
// for the life of a process and never reused the way pids are, so
// comparing a previously recorded start time against a freshly queried
// one tells a genuine surviving child apart from an unrelated process
// that happens to have been assigned the same pid later.
package procident

import "time"

// StartTime returns the OS-recorded start time of the process currently
// running as pid. It returns an error if no process with that pid
// exists.
func StartTime(pid int) (time.Time, error) {
	return startTime(pid)
}
