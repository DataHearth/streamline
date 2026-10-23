package main

import (
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
)

const (
	cgroupRoot = "/sys/fs/cgroup"
	procCgroup = "/proc/self/cgroup"

	// Headroom for the memory a cgroup counts but GOMEMLIMIT does not govern:
	// the mapped binary, modernc-sqlite's off-heap pager, the bbolt mmap and
	// anacrolix peer buffers. Setting the limit at the ceiling would make the
	// GC thrash trying to reclaim heap for an overage the heap didn't cause.
	memLimitFraction = 0.8

	// A cgroup with no memory ceiling reports one near the int64 maximum
	// (v1) or the literal string "max" (v2, a parse failure). Neither is a
	// limit worth handing the collector.
	unlimited = int64(1) << 62
)

// applyMemoryLimit points GOMEMLIMIT at the cgroup's memory ceiling and
// reports the value it set, or 0 when it set none.
//
// Go reads the cgroup CPU quota for GOMAXPROCS but leaves GOMEMLIMIT at
// MaxInt64, so a container's memory limit is invisible to the collector:
// GOGC=100 doubles the live heap before collecting and the OOM killer, not
// the GC, is what reacts. An explicit GOMEMLIMIT in the environment wins —
// the runtime already applied it and the operator meant it.
func applyMemoryLimit() int64 {
	if os.Getenv("GOMEMLIMIT") != "" {
		return 0
	}
	limit := cgroupMemoryLimit(cgroupRoot, procCgroup)
	if limit <= 0 {
		return 0
	}
	scaled := int64(float64(limit) * memLimitFraction)
	debug.SetMemoryLimit(scaled)
	return scaled
}

// cgroupMemoryLimit returns the smallest memory ceiling in bytes applying to
// this process, or 0 when there is none. Both layouts are read — v2 keeps the
// limit in memory.max, v1 in memory/memory.limit_in_bytes — and every
// candidate that fails to open is simply not a limit.
//
// Under a cgroup namespace, which is every container, the process sits at the
// mount root and the bare paths hit. On a host it does not, so the path from
// /proc/self/cgroup is walked up to the root as well: a systemd unit's limit
// is usually set on an ancestor slice rather than on the leaf.
func cgroupMemoryLimit(root, procSelf string) int64 {
	var best int64
	for _, dir := range cgroupDirs(root, selfCgroupPaths(procSelf)) {
		for _, name := range []string{"memory.max", "memory.limit_in_bytes"} {
			//nolint:gosec // cgroup paths come from the kernel and our own constants, never from a request
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
			if err != nil || v <= 0 || v >= unlimited {
				continue
			}
			if best == 0 || v < best {
				best = v
			}
		}
	}
	return best
}

// cgroupDirs lists every directory that may hold a memory limit for this
// process: the v2 and v1 mount roots, plus each ancestor of the paths the
// process is a member of under both. A directory that does not exist costs
// one failed open.
func cgroupDirs(root string, selfPaths []string) []string {
	dirs := []string{root, filepath.Join(root, "memory")}
	for _, p := range selfPaths {
		for ; p != "" && p != "/" && p != "."; p = path.Dir(p) {
			dirs = append(dirs,
				filepath.Join(root, p),
				filepath.Join(root, "memory", p),
			)
		}
	}
	return dirs
}

// selfCgroupPaths returns the cgroup paths this process belongs to as written
// in /proc/self/cgroup: the v2 unified line ("0::/path") and any v1 line
// carrying the memory controller.
func selfCgroupPaths(procSelf string) []string {
	//nolint:gosec // procSelf is /proc/self/cgroup, a caller-supplied constant
	b, err := os.ReadFile(procSelf)
	if err != nil {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.SplitN(line, ":", 3)
		if len(f) != 3 {
			continue
		}
		if f[1] == "" || slices.Contains(strings.Split(f[1], ","), "memory") {
			out = append(out, f[2])
		}
	}
	return out
}
