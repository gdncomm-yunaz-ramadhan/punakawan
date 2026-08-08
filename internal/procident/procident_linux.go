//go:build linux

package procident

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// clockTicksPerSecond is USER_HZ, the unit /proc/[pid]/stat's starttime
// field is expressed in. It is defined as 100 on every Linux
// architecture this repo targets; there is no portable way to read
// sysconf(_SC_CLK_TCK) from Go without cgo, and 100 is what every
// major distro ships.
const clockTicksPerSecond = 100

// startTime parses /proc/[pid]/stat: field 22 (starttime, in clock
// ticks since boot) is converted to wall-clock time via /proc/uptime's
// current uptime, since Linux does not expose an absolute boot time
// query as directly as starttime itself.
func startTime(pid int) (time.Time, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return time.Time{}, err
	}
	// comm (field 2) is parenthesized and may itself contain spaces or
	// parens, so skip past the *last* ')' rather than splitting naively.
	idx := strings.LastIndexByte(string(stat), ')')
	if idx < 0 || idx+2 > len(stat) {
		return time.Time{}, fmt.Errorf("procident: malformed /proc/%d/stat", pid)
	}
	// fields[0] is field 3 (state); field 22 is therefore fields[19].
	fields := strings.Fields(string(stat)[idx+2:])
	const starttimeOffset = 22 - 3
	if len(fields) <= starttimeOffset {
		return time.Time{}, fmt.Errorf("procident: /proc/%d/stat has too few fields", pid)
	}
	ticks, err := strconv.ParseInt(fields[starttimeOffset], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("procident: parse starttime for pid %d: %w", pid, err)
	}

	uptimeRaw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Time{}, err
	}
	uptimeSeconds, err := strconv.ParseFloat(strings.Fields(string(uptimeRaw))[0], 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("procident: parse /proc/uptime: %w", err)
	}

	bootTime := time.Now().Add(-time.Duration(uptimeSeconds * float64(time.Second)))
	return bootTime.Add(time.Duration(ticks) * time.Second / clockTicksPerSecond), nil
}
