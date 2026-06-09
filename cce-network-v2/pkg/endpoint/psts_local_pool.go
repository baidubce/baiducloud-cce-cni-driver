package endpoint

import (
	"bytes"
	"fmt"
	"math/big"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ipRange represents a single IP range in the local pool
type ipRange struct {
	startIP net.IP
	endIP   net.IP
	count   int // Total number of IPs in this range (endIP - startIP + 1), used for modulo indexing
}

type ipCandidateCooldown struct {
	failureCount     int
	unavailableUntil time.Time
}

const (
	candidateCooldownBase = 2 * time.Second
	candidateCooldownMax  = 2 * time.Minute
)

type pstsLocalPool struct {
	sync.RWMutex

	// --- Config fields: determined by annotation, set during buildExpectedPoolsFromAnnotation ---
	subnetID string    // Subnet this pool belongs to
	family   string    // IP family: "4" or "6"
	ranges   []ipRange // IP ranges parsed from reserved CIDRs

	// --- Runtime fields: set by initPoolRuntime when pool is actually activated ---
	poolID            string          // Format: psts|ns:{len}:{namespace}|name:{len}:{pstsName}|subnet:{len}:{subnetID}|family:{len}:{family}
	pstsNamespace     string          // PSTS namespace, used for pool ownership matching
	pstsName          string          // PSTS name, used for pool ownership matching
	currentRangeIndex int             // Current range index for round-robin allocation
	currentIndex      int             // Next allocation start position within current range (ring cursor)
	realOccupied      map[string]bool // IP -> occupied status (actual allocation state from VPC)
	candidateCooldown map[string]ipCandidateCooldown
	nowFunc           func() time.Time
	log               *logrus.Entry
}

// newExpectedPool creates a pool with config fields only (subnetID, family, ranges).
// Runtime fields are NOT set — call initPoolRuntime before putting it into active use.
func newExpectedPool(subnetID, family string, ranges []ipRange) *pstsLocalPool {
	return &pstsLocalPool{
		subnetID: subnetID,
		family:   family,
		ranges:   ranges,
	}
}

// initPoolRuntime fills in the runtime fields that are not known at annotation-parse time.
// Config fields (subnetID, family, ranges) are already populated by newExpectedPool.
func (p *pstsLocalPool) initPoolRuntime(poolID, pstsNamespace, pstsName string, log *logrus.Entry) {
	p.poolID = poolID
	p.pstsNamespace = pstsNamespace
	p.pstsName = pstsName
	p.realOccupied = make(map[string]bool)
	p.candidateCooldown = make(map[string]ipCandidateCooldown)
	p.nowFunc = time.Now
	p.log = log.WithField("poolID", poolID)
}

// addRange adds a new IP range to the pool
func (p *pstsLocalPool) addRange(startIP, endIP net.IP) {
	count := calculateIPCount(startIP, endIP)
	p.ranges = append(p.ranges, ipRange{
		startIP: startIP,
		endIP:   endIP,
		count:   count,
	})
	p.log.Infof("Added range %s-%s (%d IPs) to pool", startIP.String(), endIP.String(), count)
}

func (p *pstsLocalPool) allocateNext() (net.IP, func(), error) {
	p.Lock()
	defer p.Unlock()

	if len(p.ranges) == 0 {
		return nil, nil, fmt.Errorf("no IP ranges configured in pool")
	}

	// Try all ranges starting from current range (round-robin)
	totalRanges := len(p.ranges)
	for rangeAttempt := 0; rangeAttempt < totalRanges; rangeAttempt++ {
		currentRange := p.ranges[p.currentRangeIndex]

		// Try all IPs in current range, starting from p.currentIndex
		for i := 0; i < currentRange.count; i++ {
			idx := (p.currentIndex + i) % currentRange.count
			currentIP := getIPAtIndexInRange(currentRange.startIP, idx)
			currentIPStr := currentIP.String()

			if p.realOccupied[currentIPStr] {
				continue
			}
			if p.isInCooldownLocked(currentIPStr) {
				continue
			}

			// Found available IP - update cursor for next allocation
			p.realOccupied[currentIPStr] = true
			// Candidate passed cooldown checks and is selected again, clear stale cooldown state.
			delete(p.candidateCooldown, currentIPStr)
			nextIndex := (p.currentIndex + i + 1) % currentRange.count
			// If nextIndex wrapped to 0, it means we've happened to reach the end of current range
			// Move to next range to maintain circular allocation across all ranges
			if nextIndex == 0 {
				p.currentRangeIndex = (p.currentRangeIndex + 1) % totalRanges
			}
			p.currentIndex = nextIndex

			release := func() {
				p.Lock()
				defer p.Unlock()
				delete(p.realOccupied, currentIPStr)
			}

			return currentIP, release, nil
		}

		// Current range exhausted, move to next range
		p.currentRangeIndex = (p.currentRangeIndex + 1) % totalRanges
		p.currentIndex = 0
	}

	return nil, nil, fmt.Errorf("no available IP in any range")
}

func (p *pstsLocalPool) release(ip net.IP) {
	p.Lock()
	defer p.Unlock()
	delete(p.realOccupied, ip.String())
}

func (p *pstsLocalPool) markCandidateUnavailable(ip net.IP) (int, time.Duration, time.Time, bool) {
	if ip == nil {
		return 0, 0, time.Time{}, false
	}

	p.Lock()
	defer p.Unlock()

	if !p.isInAnyRangeLocked(ip) {
		return 0, 0, time.Time{}, false
	}

	ipStr := ip.String()
	cooldown := p.candidateCooldown[ipStr]
	cooldown.failureCount++
	backoff := computeCandidateCooldown(cooldown.failureCount)
	cooldown.unavailableUntil = p.now().Add(backoff)
	p.candidateCooldown[ipStr] = cooldown
	return cooldown.failureCount, backoff, cooldown.unavailableUntil, true
}

func (p *pstsLocalPool) resetCandidateCooldown(ip net.IP) {
	if ip == nil {
		return
	}
	p.Lock()
	defer p.Unlock()
	delete(p.candidateCooldown, ip.String())
}

// markOccupied marks ip as occupied if it belongs to current pool ranges.
// Returns true if ip was within range and marked, false otherwise.
func (p *pstsLocalPool) markOccupied(ip net.IP) bool {
	if ip == nil {
		return false
	}
	p.Lock()
	defer p.Unlock()

	if len(p.ranges) == 0 {
		return false
	}

	for _, r := range p.ranges {
		if ipInIPRange(ip, r) {
			p.realOccupied[ip.String()] = true
			delete(p.candidateCooldown, ip.String())
			return true
		}
	}
	return false
}

// updateRanges replaces the pool's ranges with newRanges, sorts them,
// and adjusts the cursor to avoid replaying already-full ranges.
func (p *pstsLocalPool) updateRanges(newRanges []ipRange) {
	p.Lock()
	defer p.Unlock()

	oldRanges := p.ranges
	oldRangeIndex := p.currentRangeIndex
	oldIndex := p.currentIndex

	p.ranges = newRanges
	sortRanges(p.ranges)
	p.pruneCooldownOutOfRangeLocked()
	p.adjustCursorAfterRangeUpdate(oldRanges, oldRangeIndex, oldIndex)
}

// rangesEqual checks if two range slices are equal (assumes both are sorted)
func rangesEqual(a, b []ipRange) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].startIP.Equal(b[i].startIP) || !a[i].endIP.Equal(b[i].endIP) {
			return false
		}
	}
	return true
}

// sortRanges sorts ipRange slices by startIP in ascending order
func sortRanges(ranges []ipRange) {
	sort.Slice(ranges, func(i, j int) bool {
		return bytes.Compare(ranges[i].startIP.To16(), ranges[j].startIP.To16()) < 0
	})
}

// ipInRange checks if ip falls within [r.startIP, r.endIP]
func ipInIPRange(ip net.IP, r ipRange) bool {
	ip16 := ip.To16()
	return bytes.Compare(ip16, r.startIP.To16()) >= 0 && bytes.Compare(ip16, r.endIP.To16()) <= 0
}

// adjustCursorAfterRangeUpdate locates the old cursor IP in the new ranges.
// If found, cursor continues from that position. If the old range was deleted,
// cursor moves to the next range (by startIP order) to avoid replaying
// already-full ranges from the beginning.
//
// Must be called with pool lock held.
func (p *pstsLocalPool) adjustCursorAfterRangeUpdate(oldRanges []ipRange, oldRangeIndex, oldIndex int) {
	// Defensive: oldRangeIndex out of bounds means cursor state was corrupted
	if oldRangeIndex >= len(oldRanges) {
		p.log.Warnf("adjustCursorAfterRangeUpdate: oldRangeIndex %d >= len(oldRanges) %d, resetting cursor", oldRangeIndex, len(oldRanges))
		p.currentRangeIndex = 0
		p.currentIndex = 0
		return
	}
	oldRange := oldRanges[oldRangeIndex]
	cursorIP := getIPAtIndexInRange(oldRange.startIP, oldIndex)

	// Case 1: cursor IP still falls inside one of the new ranges
	for i, r := range p.ranges {
		if ipInIPRange(cursorIP, r) {
			p.currentRangeIndex = i
			// Recalculate index relative to new range's startIP
			// (range may have been extended forward, so oldIndex is no longer correct)
			p.currentIndex = calculateIPCount(r.startIP, cursorIP) - 1
			return
		}
	}

	// Case 2: old range was deleted — find the next range whose startIP > oldRange.startIP
	for i, r := range p.ranges {
		if bytes.Compare(r.startIP.To16(), oldRange.startIP.To16()) > 0 {
			p.currentRangeIndex = i
			p.currentIndex = 0
			return
		}
	}

	// Case 3: old range and all ranges after it were deleted — wrap to first range
	p.currentRangeIndex = 0
	p.currentIndex = 0
}

func (p *pstsLocalPool) isInCooldownLocked(ip string) bool {
	cooldown, ok := p.candidateCooldown[ip]
	if !ok {
		return false
	}
	return p.now().Before(cooldown.unavailableUntil)
}

func (p *pstsLocalPool) isInAnyRangeLocked(ip net.IP) bool {
	for _, r := range p.ranges {
		if ipInIPRange(ip, r) {
			return true
		}
	}
	return false
}

func (p *pstsLocalPool) pruneCooldownOutOfRangeLocked() {
	for ipStr := range p.candidateCooldown {
		ip := net.ParseIP(ipStr)
		if ip == nil || !p.isInAnyRangeLocked(ip) {
			delete(p.candidateCooldown, ipStr)
		}
	}
}

func (p *pstsLocalPool) now() time.Time {
	if p.nowFunc == nil {
		return time.Now()
	}
	return p.nowFunc()
}

func computeCandidateCooldown(failureCount int) time.Duration {
	if failureCount <= 1 {
		return candidateCooldownBase
	}

	backoff := candidateCooldownBase
	for i := 1; i < failureCount; i++ {
		if backoff >= candidateCooldownMax {
			return candidateCooldownMax
		}
		backoff *= 2
	}

	if backoff > candidateCooldownMax {
		return candidateCooldownMax
	}
	return backoff
}

func calculateIPCount(startIP, endIP net.IP) int {
	// Detect IP version
	if startIP.To4() != nil {
		// IPv4
		start := ipv4ToInt(startIP)
		end := ipv4ToInt(endIP)
		return int(end - start + 1)
	} else {
		// IPv6
		start := ipv6ToBigInt(startIP)
		end := ipv6ToBigInt(endIP)
		count := new(big.Int).Sub(end, start)
		count.Add(count, big.NewInt(1))
		// For large IPv6 ranges, cap at max int
		if !count.IsInt64() {
			return int(^uint(0) >> 1) // max int
		}
		return int(count.Int64())
	}
}

// getIPAtIndexInRange returns the IP at given index within a range
func getIPAtIndexInRange(startIP net.IP, index int) net.IP {
	if startIP.To4() != nil {
		// IPv4
		offset := uint32(index)
		return intToIPv4(ipv4ToInt(startIP) + offset)
	} else {
		// IPv6
		start := ipv6ToBigInt(startIP)
		offset := big.NewInt(int64(index))
		result := new(big.Int).Add(start, offset)
		return bigIntToIPv6(result)
	}
}

func ipv4ToInt(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func intToIPv4(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func ipv6ToBigInt(ip net.IP) *big.Int {
	ip = ip.To16()
	return new(big.Int).SetBytes(ip)
}

func bigIntToIPv6(n *big.Int) net.IP {
	bytes := n.Bytes()
	// Pad to 16 bytes if needed
	if len(bytes) < 16 {
		padded := make([]byte, 16)
		copy(padded[16-len(bytes):], bytes)
		bytes = padded
	}
	return net.IP(bytes)
}
