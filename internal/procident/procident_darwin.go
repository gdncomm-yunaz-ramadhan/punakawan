//go:build darwin

package procident

import (
	"time"

	"golang.org/x/sys/unix"
)

func startTime(pid int) (time.Time, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, err
	}
	sec := int64(kp.Proc.P_starttime.Sec)
	usec := int64(kp.Proc.P_starttime.Usec)
	return time.Unix(sec, usec*1000), nil
}
