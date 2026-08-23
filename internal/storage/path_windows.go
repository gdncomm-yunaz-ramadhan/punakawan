package storage

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func isNetworkMount(dir string) (bool, string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false, "", err
	}
	root := filepath.VolumeName(abs) + `\`
	ptr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false, "", err
	}
	if windows.GetDriveType(ptr) == windows.DRIVE_REMOTE {
		return true, "remote", nil
	}
	return false, "", nil
}
