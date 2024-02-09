package netns

import (
	"testing"
)

func TestGetProcNSPath(t *testing.T) {

	testNamespacePath := "/proc/123456/ns/net"
	nspath, err := GetProcNSPath(testNamespacePath)
	if err == nil && nspath == testNamespacePath {
		return
	}
}
