package endpoint

import (
	"net"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newTestPool() *pstsLocalPool {
	log := logrus.NewEntry(logrus.New())
	pool := newExpectedPool("subnet-1", "4", nil)
	pool.initPoolRuntime("test-pool", "test-ns", "test-psts", log)
	return pool
}

// TestAllocateNextCircularAcrossRanges verifies that allocation circularly moves across ranges
func TestAllocateNextCircularAcrossRanges(t *testing.T) {
	pool := newTestPool()

	// Add two ranges: 10.0.0.1-10.0.0.2 (2 IPs) and 10.0.1.1-10.0.1.2 (2 IPs)
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))
	pool.addRange(net.ParseIP("10.0.1.1"), net.ParseIP("10.0.1.2"))

	// Expected allocation order (circular across all ranges):
	// 10.0.0.1 -> 10.0.0.2 -> (range switch) -> 10.0.1.1 -> 10.0.1.2 -> (wrap) -> 10.0.0.1 ...
	expected := []string{"10.0.0.1", "10.0.0.2", "10.0.1.1", "10.0.1.2"}

	for i, exp := range expected {
		ip, release, err := pool.allocateNext()
		if err != nil {
			t.Fatalf("allocation %d failed: %v", i, err)
		}
		if ip.String() != exp {
			t.Errorf("allocation %d: expected %s, got %s", i, exp, ip.String())
		}
		// Release immediately to allow re-allocation
		release()
	}

	// Verify cursor moved to range 0 after completing the cycle
	if pool.currentRangeIndex != 0 {
		t.Errorf("expected currentRangeIndex=0 after full cycle, got %d", pool.currentRangeIndex)
	}
	if pool.currentIndex != 0 {
		t.Errorf("expected currentIndex=0 after full cycle, got %d", pool.currentIndex)
	}
}

// TestAllocateNextCursorPosition verifies cursor position after each allocation
func TestAllocateNextCursorPosition(t *testing.T) {
	pool := newTestPool()

	// Range with 3 IPs
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.3"))
	// Range with 2 IPs
	pool.addRange(net.ParseIP("10.0.1.1"), net.ParseIP("10.0.1.2"))

	testCases := []struct {
		expectedIP         string
		expectedRangeIndex int
		expectedIndex      int
	}{
		{"10.0.0.1", 0, 1},
		{"10.0.0.2", 0, 2},
		{"10.0.0.3", 1, 0}, // After last IP in range 0, should move to range 1
		{"10.0.1.1", 1, 1},
		{"10.0.1.2", 0, 0}, // After last IP in range 1, should wrap to range 0
	}

	for i, tc := range testCases {
		ip, release, err := pool.allocateNext()
		if err != nil {
			t.Fatalf("allocation %d failed: %v", i, err)
		}
		if ip.String() != tc.expectedIP {
			t.Errorf("allocation %d: expected IP %s, got %s", i, tc.expectedIP, ip.String())
		}
		if pool.currentRangeIndex != tc.expectedRangeIndex {
			t.Errorf("allocation %d: expected rangeIndex %d, got %d", i, tc.expectedRangeIndex, pool.currentRangeIndex)
		}
		if pool.currentIndex != tc.expectedIndex {
			t.Errorf("allocation %d: expected index %d, got %d", i, tc.expectedIndex, pool.currentIndex)
		}
		release()
	}
}

// TestAllocateNextSkipsOccupied verifies that occupied IPs are skipped
func TestAllocateNextSkipsOccupied(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.3"))

	// Allocate first IP and keep it
	ip1, _, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("first allocation failed: %v", err)
	}
	if ip1.String() != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", ip1.String())
	}

	// Allocate second IP
	ip2, release2, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("second allocation failed: %v", err)
	}
	if ip2.String() != "10.0.0.2" {
		t.Errorf("expected 10.0.0.2, got %s", ip2.String())
	}
	release2()

	// Allocate third IP
	ip3, _, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("third allocation failed: %v", err)
	}
	if ip3.String() != "10.0.0.3" {
		t.Errorf("expected 10.0.0.3, got %s", ip3.String())
	}

	// Now cursor wraps, next allocation should skip 10.0.0.1 (still occupied) and get 10.0.0.2
	ip4, _, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("fourth allocation failed: %v", err)
	}
	if ip4.String() != "10.0.0.2" {
		t.Errorf("expected 10.0.0.2 (skipping occupied 10.0.0.1), got %s", ip4.String())
	}
}

func TestAllocateNextNoRanges(t *testing.T) {
	pool := newTestPool()
	_, _, err := pool.allocateNext()
	if err == nil {
		t.Fatalf("expected error when no ranges configured")
	}
}

func TestAllocateNextAllOccupied(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))

	_, _, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("first allocation failed: %v", err)
	}
	_, _, err = pool.allocateNext()
	if err == nil {
		t.Fatalf("expected error when all IPs occupied")
	}
}

func TestReleaseAllowsReallocate(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))

	ip1, release, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("first allocation failed: %v", err)
	}
	release()

	ip2, _, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("second allocation failed: %v", err)
	}
	if ip1.String() != ip2.String() {
		t.Fatalf("expected reallocate same ip, got %s and %s", ip1.String(), ip2.String())
	}
}

func TestMarkOccupied(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))

	if pool.markOccupied(nil) {
		t.Fatalf("expected false for nil ip")
	}

	if !pool.markOccupied(net.ParseIP("10.0.0.1")) {
		t.Fatalf("expected to mark ip in range")
	}
	if pool.markOccupied(net.ParseIP("10.0.1.1")) {
		t.Fatalf("expected false for ip outside range")
	}
}

func TestUpdateRangesAdjustCursor(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))
	pool.currentRangeIndex = 0
	pool.currentIndex = 1 // cursor at 10.0.0.2

	newRanges := []ipRange{{
		startIP: net.ParseIP("10.0.0.0"),
		endIP:   net.ParseIP("10.0.0.2"),
		count:   3,
	}}
	pool.updateRanges(newRanges)

	if pool.currentRangeIndex != 0 {
		t.Fatalf("expected range index 0, got %d", pool.currentRangeIndex)
	}
	if pool.currentIndex != 2 {
		t.Fatalf("expected index 2, got %d", pool.currentIndex)
	}
}

func TestAdjustCursorAfterRangeUpdateDeletedRange(t *testing.T) {
	pool := newTestPool()
	oldRanges := []ipRange{{
		startIP: net.ParseIP("10.0.0.1"),
		endIP:   net.ParseIP("10.0.0.2"),
		count:   2,
	}}
	pool.ranges = []ipRange{{
		startIP: net.ParseIP("10.0.2.1"),
		endIP:   net.ParseIP("10.0.2.1"),
		count:   1,
	}}
	pool.adjustCursorAfterRangeUpdate(oldRanges, 0, 0)

	if pool.currentRangeIndex != 0 || pool.currentIndex != 0 {
		t.Fatalf("expected cursor reset to 0,0 got %d,%d", pool.currentRangeIndex, pool.currentIndex)
	}
}

func TestCalculateIPCountIPv6(t *testing.T) {
	start := net.ParseIP("2001:db8::1")
	end := net.ParseIP("2001:db8::3")
	if got := calculateIPCount(start, end); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestGetIPAtIndexInRangeIPv6(t *testing.T) {
	start := net.ParseIP("2001:db8::1")
	ip := getIPAtIndexInRange(start, 1)
	if ip.String() != "2001:db8::2" {
		t.Fatalf("expected 2001:db8::2, got %s", ip.String())
	}
}

func TestRangesEqual(t *testing.T) {
	a := []ipRange{{
		startIP: net.ParseIP("10.0.0.1"),
		endIP:   net.ParseIP("10.0.0.2"),
	}}
	b := []ipRange{{
		startIP: net.ParseIP("10.0.0.1"),
		endIP:   net.ParseIP("10.0.0.2"),
	}}
	if !rangesEqual(a, b) {
		t.Fatalf("expected ranges to be equal")
	}
	b[0].endIP = net.ParseIP("10.0.0.3")
	if rangesEqual(a, b) {
		t.Fatalf("expected ranges to be different")
	}
}

func TestSortRanges(t *testing.T) {
	ranges := []ipRange{
		{startIP: net.ParseIP("10.0.0.10"), endIP: net.ParseIP("10.0.0.10")},
		{startIP: net.ParseIP("10.0.0.1"), endIP: net.ParseIP("10.0.0.1")},
	}
	sortRanges(ranges)
	if ranges[0].startIP.String() != "10.0.0.1" {
		t.Fatalf("expected first range to start at 10.0.0.1, got %s", ranges[0].startIP)
	}
}

func TestIPInIPRange(t *testing.T) {
	rng := ipRange{startIP: net.ParseIP("10.0.0.1"), endIP: net.ParseIP("10.0.0.3")}
	if !ipInIPRange(net.ParseIP("10.0.0.2"), rng) {
		t.Fatalf("expected ip in range")
	}
	if ipInIPRange(net.ParseIP("10.0.0.4"), rng) {
		t.Fatalf("expected ip outside range")
	}
}

func TestCalculateIPCountIPv4(t *testing.T) {
	start := net.ParseIP("10.0.0.1")
	end := net.ParseIP("10.0.0.3")
	if got := calculateIPCount(start, end); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestGetIPAtIndexInRangeIPv4(t *testing.T) {
	start := net.ParseIP("10.0.0.1")
	ip := getIPAtIndexInRange(start, 2)
	if ip.String() != "10.0.0.3" {
		t.Fatalf("expected 10.0.0.3, got %s", ip.String())
	}
}

func TestIPv4Conversions(t *testing.T) {
	ip := net.ParseIP("10.1.2.3")
	n := ipv4ToInt(ip)
	round := intToIPv4(n)
	if round.String() != "10.1.2.3" {
		t.Fatalf("expected 10.1.2.3, got %s", round.String())
	}
}

func TestIPv6Conversions(t *testing.T) {
	ip := net.ParseIP("2001:db8::1")
	n := ipv6ToBigInt(ip)
	round := bigIntToIPv6(n)
	if round.String() != "2001:db8::1" {
		t.Fatalf("expected 2001:db8::1, got %s", round.String())
	}
}

func TestCandidateCooldownSkipsUntilExpiry(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))

	now := time.Date(2026, 3, 17, 9, 0, 0, 0, time.UTC)
	pool.nowFunc = func() time.Time { return now }

	failures, backoff, _, ok := pool.markCandidateUnavailable(net.ParseIP("10.0.0.1"))
	if !ok {
		t.Fatalf("expected markCandidateUnavailable success")
	}
	if failures != 1 || backoff != candidateCooldownBase {
		t.Fatalf("unexpected cooldown result failures=%d backoff=%v", failures, backoff)
	}

	ip, release, err := pool.allocateNext()
	if err != nil {
		t.Fatalf("allocate with cooldown failed: %v", err)
	}
	if ip.String() != "10.0.0.2" {
		t.Fatalf("expected 10.0.0.2 while 10.0.0.1 is cooling down, got %s", ip.String())
	}
	release()

	now = now.Add(candidateCooldownBase - time.Millisecond)
	ip, release, err = pool.allocateNext()
	if err != nil {
		t.Fatalf("allocate before cooldown expiry failed: %v", err)
	}
	if ip.String() != "10.0.0.2" {
		t.Fatalf("expected 10.0.0.2 before cooldown expiry, got %s", ip.String())
	}
	release()

	now = now.Add(2 * time.Millisecond)
	ip, _, err = pool.allocateNext()
	if err != nil {
		t.Fatalf("allocate after cooldown expiry failed: %v", err)
	}
	if ip.String() != "10.0.0.1" {
		t.Fatalf("expected 10.0.0.1 after cooldown expiry, got %s", ip.String())
	}
}

func TestCandidateCooldownExponentialBackoff(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))

	now := time.Date(2026, 3, 17, 9, 0, 0, 0, time.UTC)
	pool.nowFunc = func() time.Time { return now }

	failures, backoff, until, ok := pool.markCandidateUnavailable(net.ParseIP("10.0.0.1"))
	if !ok || failures != 1 || backoff != 2*time.Second || !until.Equal(now.Add(2*time.Second)) {
		t.Fatalf("unexpected first cooldown result failures=%d backoff=%v until=%v ok=%v", failures, backoff, until, ok)
	}

	failures, backoff, until, ok = pool.markCandidateUnavailable(net.ParseIP("10.0.0.1"))
	if !ok || failures != 2 || backoff != 4*time.Second || !until.Equal(now.Add(4*time.Second)) {
		t.Fatalf("unexpected second cooldown result failures=%d backoff=%v until=%v ok=%v", failures, backoff, until, ok)
	}

	for i := 0; i < 10; i++ {
		failures, backoff, until, ok = pool.markCandidateUnavailable(net.ParseIP("10.0.0.1"))
		if !ok {
			t.Fatalf("markCandidateUnavailable should keep succeeding")
		}
	}
	if backoff != candidateCooldownMax || !until.Equal(now.Add(candidateCooldownMax)) {
		t.Fatalf("expected cooldown cap=%v, got backoff=%v until=%v failures=%d", candidateCooldownMax, backoff, until, failures)
	}
}

func TestMarkOccupiedClearsCandidateCooldown(t *testing.T) {
	pool := newTestPool()
	pool.addRange(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.1"))

	_, _, _, ok := pool.markCandidateUnavailable(net.ParseIP("10.0.0.1"))
	if !ok {
		t.Fatalf("expected markCandidateUnavailable success")
	}
	if len(pool.candidateCooldown) != 1 {
		t.Fatalf("expected one cooldown entry, got %d", len(pool.candidateCooldown))
	}

	if !pool.markOccupied(net.ParseIP("10.0.0.1")) {
		t.Fatalf("expected markOccupied success")
	}
	if len(pool.candidateCooldown) != 0 {
		t.Fatalf("expected cooldown cleared after markOccupied, got %d", len(pool.candidateCooldown))
	}
}
