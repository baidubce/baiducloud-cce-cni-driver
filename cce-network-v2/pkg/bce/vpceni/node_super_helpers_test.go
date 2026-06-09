package vpceni

import (
	"testing"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/stretchr/testify/require"
)

func TestAppendUniqueElement(t *testing.T) {
	arr := []*models.PrivateIP{{PrivateIPAddress: "10.0.0.1"}}
	toadd := []*models.PrivateIP{{PrivateIPAddress: "10.0.0.1"}, {PrivateIPAddress: "10.0.0.2"}}
	result := appenUniqueElement(arr, toadd)
	require.Len(t, result, 2)
	require.Equal(t, "10.0.0.1", result[0].PrivateIPAddress)
	require.Equal(t, "10.0.0.2", result[1].PrivateIPAddress)
}

func TestIPToReleaseSetAppend(t *testing.T) {
	set := ipToReleaseSet{}
	set = set.append("1", 2)
	require.Len(t, set, 1)
	require.Equal(t, []string{"1"}, set[0])

	set = set.append("2", 2)
	require.Len(t, set, 1)
	require.Equal(t, []string{"1", "2"}, set[0])

	set = set.append("3", 2)
	require.Len(t, set, 2)
	require.Equal(t, []string{"3"}, set[1])
}

func TestCreatingEniSet(t *testing.T) {
	s := newCreatingEniSet()
	require.False(t, s.hasCreatingENI())

	createTime := time.Now().Add(-2 * time.Second)
	s.addCreatingENI("eni-1", createTime)
	require.True(t, s.hasCreatingENI())

	s.removeCreatingENI("eni-1")
	require.False(t, s.hasCreatingENI())

	s.add(1)
	require.True(t, s.hasCreatingENI())

	s.creatingENINums = 0
	s.addCreatingENI("eni-2", time.Now().Add(-10*time.Second))
	s.cleanExpiredCreatingENI([]string{}, 2*time.Second)
	require.False(t, s.hasCreatingENI())
}
