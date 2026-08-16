//go:build windows

package daemon

// checkTokenFilePermissions is a no-op on Windows: Unix mode bits do not
// meaningfully express NTFS ACLs, and this repo has no existing
// Windows ACL-inspection code to build on. Enforcing owner-only access
// via GetNamedSecurityInfo/DACL comparison is tracked as follow-up work
// rather than approximated here.
func checkTokenFilePermissions(path string) error {
	return nil
}
