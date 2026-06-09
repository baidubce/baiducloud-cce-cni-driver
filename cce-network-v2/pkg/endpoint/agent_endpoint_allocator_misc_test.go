//go:build linux
// +build linux

package endpoint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEndpointAllocatorDumpDebugRestore(t *testing.T) {
	mockIPAM := &MockIPAMAllocator{}
	mockIPAM.On("Dump").Return(map[string]string{"10.0.0.1": "ns/pod"}, map[string]string{}, "ok")
	mockIPAM.On("DebugStatus").Return("debug")
	mockIPAM.On("RestoreFinished").Return()

	ea := &EndpointAllocator{dynamicIPAM: mockIPAM}

	v4, v6, status := ea.Dump()
	require.Equal(t, "ns/pod", v4["10.0.0.1"])
	require.Empty(t, v6)
	require.Equal(t, "ok", status)

	require.Equal(t, "debug", ea.DebugStatus())
	ea.RestoreFinished()

	mockIPAM.AssertExpectations(t)
}

func TestEndpointAllocatorMacAndModes(t *testing.T) {
	ea := &EndpointAllocator{rdmaAttr: rdmaEndpointAllocatorAttr{isRdmaEndpointAllocator: true, primaryMacAddress: "old", ipamType: "underlay"}}
	require.True(t, ea.isRDMAMode())
	require.True(t, ea.isRDMAIPAM())
	require.Equal(t, "underlay", ea.GetIPAMType())
	require.Equal(t, "old", ea.GetPrimaryMacAddress())

	ea.UpdatePrimaryMacAddress("new")
	require.Equal(t, "new", ea.GetPrimaryMacAddress())

	non := &EndpointAllocator{rdmaAttr: rdmaEndpointAllocatorAttr{isRdmaEndpointAllocator: false, primaryMacAddress: "old", ipamType: ""}}
	require.False(t, non.isRDMAMode())
	require.False(t, non.isRDMAIPAM())
	require.Equal(t, "", non.GetIPAMType())

	non.UpdatePrimaryMacAddress("new")
	require.Equal(t, "old", non.GetPrimaryMacAddress())
}
