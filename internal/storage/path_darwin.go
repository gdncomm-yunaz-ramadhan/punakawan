package storage

import "syscall"

var networkFSTypesDarwin = map[string]bool{
	"nfs": true, "smbfs": true, "afpfs": true, "webdav": true, "ftp": true,
}

func isNetworkMount(dir string) (bool, string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return false, "", err
	}
	b := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	fsType := string(b)
	return networkFSTypesDarwin[fsType], fsType, nil
}
