// procstart.go — when did the node's containerd start? Go programs
// (containerd among them) load the system root CAs once, at first use, so a
// CA appended to the host bundle only reaches a containerd that started
// after it landed. The agent reads that from the host's /proc, mounted
// read-only into the pod (the node-exporter pattern). That is narrower than
// hostPID: the agent can see host processes but shares no PID namespace with
// them, so it cannot signal or ptrace them.
package nodeagent

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// containerdComms are the process names (/proc/<pid>/comm) that may hold
// containerd's CRI image service, most specific first. k3s runs containerd
// as a child named "containerd" that dies with k3s (Pdeathsig), so k3s's own
// process is only a fallback for builds that embed it. The first name with
// any match wins.
var containerdComms = [][]string{
	{"containerd"},
	{"k3s-agent", "k3s-server", "k3s"},
}

// userHZ is the unit of /proc/<pid>/stat starttime. The kernel exports
// USER_HZ, which is 100 on every architecture Linux supports, so it is not
// worth cgo for sysconf(_SC_CLK_TCK).
const userHZ = 100

// containerdStart returns the start time of the oldest process carrying the
// first matching name in containerdComms, and that name. Oldest, because if
// two run (e.g. Docker's containerd beside k3s's) either may be pulling, and
// the older one is the one that may have loaded stale roots.
func containerdStart(procDir string) (time.Time, string, error) {
	if procDir == "" {
		return time.Time{}, "", errors.New("no host /proc configured")
	}
	boot, err := bootTime(procDir)
	if err != nil {
		return time.Time{}, "", err
	}
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("list %s: %w", procDir, err)
	}
	type proc struct {
		comm  string
		ticks uint64
	}
	var procs []proc
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		// Processes exit mid-scan; an unreadable entry is just skipped.
		stat, err := os.ReadFile(filepath.Join(procDir, e.Name(), "stat"))
		if err != nil {
			continue
		}
		if comm, ticks, ok := parseStat(string(stat)); ok {
			procs = append(procs, proc{comm, ticks})
		}
	}
	for _, names := range containerdComms {
		var oldest *proc
		for i, p := range procs {
			for _, n := range names {
				if p.comm == n && (oldest == nil || p.ticks < oldest.ticks) {
					oldest = &procs[i]
				}
			}
		}
		if oldest != nil {
			since := time.Duration(oldest.ticks) * (time.Second / userHZ)
			return boot.Add(since), oldest.comm, nil
		}
	}
	return time.Time{}, "", fmt.Errorf("no containerd or k3s process in %s", procDir)
}

// parseStat extracts comm and starttime (field 22) from /proc/<pid>/stat.
// comm is parenthesised and may itself contain spaces or ")", so fields are
// counted from the last ")".
func parseStat(stat string) (comm string, startTicks uint64, ok bool) {
	open, closing := strings.IndexByte(stat, '('), strings.LastIndexByte(stat, ')')
	if open < 0 || closing < open {
		return "", 0, false
	}
	// After ")" come fields 3 (state) onward; starttime is field 22.
	rest := strings.Fields(stat[closing+1:])
	if len(rest) < 20 {
		return "", 0, false
	}
	ticks, err := strconv.ParseUint(rest[19], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return stat[open+1 : closing], ticks, true
}

// bootTime is the "btime" line of /proc/stat: the wall-clock boot time, to
// which process start ticks are relative.
func bootTime(procDir string) (time.Time, error) {
	f, err := os.Open(filepath.Join(procDir, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "btime "); ok {
			secs, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("parse btime %q: %w", v, err)
			}
			return time.Unix(secs, 0), nil
		}
	}
	if err := s.Err(); err != nil {
		return time.Time{}, err
	}
	return time.Time{}, fmt.Errorf("no btime in %s/stat", procDir)
}
