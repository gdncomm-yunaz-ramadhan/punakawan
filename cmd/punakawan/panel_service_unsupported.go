//go:build !darwin

package main

import "runtime"

// newPanelServiceManager refuses rather than pretending. Registering a
// background service is entirely platform-specific, and a stub that
// silently did nothing would leave the user believing the panel starts
// at login when it does not - so the failure is loud and names the
// mechanism they would need instead.
func newPanelServiceManager() (panelServiceManager, error) {
	return nil, errPanelServiceUnsupported(runtime.GOOS)
}
