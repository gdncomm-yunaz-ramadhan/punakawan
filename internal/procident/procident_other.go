//go:build !darwin && !linux && !windows

package procident

import (
	"fmt"
	"time"
)

func startTime(pid int) (time.Time, error) {
	return time.Time{}, fmt.Errorf("procident: StartTime is not implemented on this platform")
}
