package host

import (
	"cmp"
	"fmt"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

const (
	// bytesPerWorker is the RAM budget assumed for one privileged DinD.
	bytesPerWorker = 2 << 30
	// maxAutoWorkers is the hard cap for auto-detected pool size.
	maxAutoWorkers = 8
	// minWorkers is the floor.
	minWorkers = 1
)

// Snapshot is one reading of host capacity and the current pool.
type Snapshot struct {
	// CPUs is logical CPU count.
	CPUs int `json:"cpus"`
	// MemoryTotalBytes is installed RAM.
	MemoryTotalBytes uint64 `json:"memory_total_bytes"`
	// MemoryAvailBytes is currently available RAM.
	MemoryAvailBytes uint64 `json:"memory_avail_bytes"`
	// DiskAvailBytes is free space on the module's volume.
	DiskAvailBytes uint64 `json:"disk_avail_bytes"`
	// DiskPath is the path that was measured.
	DiskPath string `json:"disk_path,omitempty"`
	// RecommendedWorkers is Discover's suggested DinD count.
	RecommendedWorkers int `json:"recommended_workers"`
	// MaxWorkers is the auto-detect cap.
	MaxWorkers int `json:"max_workers"`
	// BusyWorkers is how many DinDs currently hold a case. Filled by the pool.
	BusyWorkers int `json:"busy_workers"`
	// IdleWorkers is how many DinDs are started and free.
	IdleWorkers int `json:"idle_workers"`
	// PoolSize is BusyWorkers + IdleWorkers.
	PoolSize int `json:"pool_size"`
}

// Discover samples the host and recommends a worker count.
//
// Parameters:
//   - diskPath: Filesystem to measure. Empty uses "/".
//
// Returns:
//   - Snapshot: Capacity reading.
//   - error: Probe failure.
func Discover(diskPath string) (Snapshot, error) {
	diskPath = cmp.Or(diskPath, "/")

	cpus, cpuErr := cpu.Counts(true)
	if cpuErr != nil {
		return Snapshot{}, fmt.Errorf("cpu counts: %w", cpuErr)
	}

	vm, memErr := mem.VirtualMemory()
	if memErr != nil {
		return Snapshot{}, fmt.Errorf("memory: %w", memErr)
	}

	du, diskErr := disk.Usage(diskPath)
	if diskErr != nil {
		return Snapshot{}, fmt.Errorf("disk %s: %w", diskPath, diskErr)
	}

	snap := Snapshot{
		CPUs:               cpus,
		MemoryTotalBytes:   vm.Total,
		MemoryAvailBytes:   vm.Available,
		DiskAvailBytes:     du.Free,
		DiskPath:           diskPath,
		MaxWorkers:         maxAutoWorkers,
		RecommendedWorkers: Recommend(cpus, vm.Available),
	}

	return snap, nil
}

// Recommend computes a worker count from CPU and available RAM.
//
// Parameters:
//   - cpus: Logical CPUs.
//   - memAvail: Available bytes.
//
// Returns:
//   - int: Workers in [1, 8].
func Recommend(cpus int, memAvail uint64) int {
	byCPU := max(cpus/2, minWorkers)

	byMem := minWorkers
	if memAvail >= bytesPerWorker {
		byMem = int(memAvail / bytesPerWorker)
	}

	return min(byCPU, byMem, maxAutoWorkers)
}

// ResolveWorkers picks the pool size from an explicit request or discovery.
//
// Parameters:
//   - requested: CLI --workers. Values below 1 mean auto.
//   - recommended: Discover result.
//
// Returns:
//   - int: Pool size >= 1.
func ResolveWorkers(requested, recommended int) int {
	if requested > 0 {
		return requested
	}

	if recommended < minWorkers {
		return minWorkers
	}

	return recommended
}

// CapWorkers reduces the pool when the sitting's job count is known and smaller.
//
// Parameters:
//   - requested: Size after ResolveWorkers.
//   - knownJobs: Cases that will run. Zero means unbounded.
//
// Returns:
//   - int: Pool size >= 1.
func CapWorkers(requested, knownJobs int) int {
	n := max(requested, minWorkers)
	if knownJobs > 0 {
		n = min(n, knownJobs)
	}

	return max(n, minWorkers)
}
