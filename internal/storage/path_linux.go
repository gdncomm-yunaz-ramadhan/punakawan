package storage

import "syscall"

func isNetworkMount(dir string) (bool, string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return false, "", err
	}
	// Magic numbers per statfs(2) / linux/magic.h.
	switch int64(st.Type) {
	case 0x6969:
		return true, "nfs", nil
	case 0x517B:
		return true, "smb", nil
	case 0xFF534D42:
		return true, "cifs", nil
	case 0x5346414F:
		return true, "afs", nil
	case 0x65735546:
		return true, "fuse.sshfs", nil
	default:
		return false, "", nil
	}
}
