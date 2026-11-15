package containers

import (
	"os"
	"strconv"
	"strings"
)

// Each app container gets limits, so one app that leaks memory or forks
// without end can't take the server, the dashboard and the other apps down
// with it (NIST SP 800-190 4.4.3).

// appPidsLimit bounds the processes and threads of one app.
const appPidsLimit = 1024

// minimumAppMemory keeps the limit usable on a very small server.
const minimumAppMemory = 256 << 20

// limitArgs are the podman run flags for an app's limits: half the server's
// memory, without swap beyond it, and appPidsLimit processes.
func limitArgs() []string {
	memory := strconv.FormatInt(appMemoryLimit(totalMemory()), 10)
	return []string{"--memory", memory, "--memory-swap", memory, "--pids-limit", strconv.Itoa(appPidsLimit)}
}

func appMemoryLimit(total int64) int64 {
	return max(total/2, minimumAppMemory)
}

func totalMemory() int64 {
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(string(meminfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kibibytes, _ := strconv.ParseInt(fields[1], 10, 64)
			return kibibytes * 1024
		}
	}

	return 0
}
