package initflow

import (
	"strconv"
	"strings"
)

// subIDCount is the size of tero's subordinate uid/gid range. Rootless
// BuildKit nests its own user namespace inside the container and maps ids
// 100000-165535 there, so the usual 65536 is too small.
const subIDCount = 1_000_000

// subIDAlignment keeps ranges on 65536 boundaries, like useradd does.
const subIDAlignment = 65536

// minimumSubIDStart keeps tero's range clear of regular user and group ids.
const minimumSubIDStart = 100000

type subIDRange struct {
	owner string
	start int
	count int
}

func (r subIDRange) end() int {
	return r.start + r.count
}

// parseSubIDs parses /etc/subuid or /etc/subgid.
func parseSubIDs(contents string) []subIDRange {
	var ranges []subIDRange
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Split(strings.TrimSpace(line), ":")
		if len(fields) != 3 {
			continue
		}

		start, startErr := strconv.Atoi(fields[1])
		count, countErr := strconv.Atoi(fields[2])
		if startErr != nil || countErr != nil {
			continue
		}

		ranges = append(ranges, subIDRange{owner: fields[0], start: start, count: count})
	}

	return ranges
}

// nextFreeSubIDStart returns the first aligned start, after every range
// already in use by other owners, where a range of subIDCount ids fits.
func nextFreeSubIDStart(ranges []subIDRange, owner string) int {
	start := minimumSubIDStart
	for _, existing := range ranges {
		if existing.owner == owner {
			continue
		}
		start = max(start, existing.end())
	}

	return alignUp(start, subIDAlignment)
}

func alignUp(value, alignment int) int {
	remainder := value % alignment
	if remainder == 0 {
		return value
	}

	return value + alignment - remainder
}
