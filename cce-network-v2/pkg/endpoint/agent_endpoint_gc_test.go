//go:build linux
// +build linux

package endpoint

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
)

type gcTestConfig struct {
	gcInterval        time.Duration
	fixedIPTimeout    time.Duration
	ipv4Enabled       bool
	ipv6Enabled       bool
	ipamMode          string
	nativeRoutingCIDR *cidr.CIDR
}

func (c *gcTestConfig) IPAMMode() string                      { return c.ipamMode }
func (c *gcTestConfig) IPv4Enabled() bool                     { return c.ipv4Enabled }
func (c *gcTestConfig) IPv6Enabled() bool                     { return c.ipv6Enabled }
func (c *gcTestConfig) HealthCheckingEnabled() bool           { return false }
func (c *gcTestConfig) UnreachableRoutesEnabled() bool        { return false }
func (c *gcTestConfig) SetIPv4NativeRoutingCIDR(n *cidr.CIDR) { c.nativeRoutingCIDR = n }
func (c *gcTestConfig) GetIPv4NativeRoutingCIDR() *cidr.CIDR  { return c.nativeRoutingCIDR }
func (c *gcTestConfig) GetCCEEndpointGC() time.Duration       { return c.gcInterval }
func (c *gcTestConfig) GetFixedIPTimeout() time.Duration      { return c.fixedIPTimeout }

type gcTestIPAM struct {
	allocv4  map[string]string
	allocv6  map[string]string
	status   string
	released map[string]bool
}

func (m *gcTestIPAM) DebugStatus() string                         { return "" }
func (m *gcTestIPAM) RestoreFinished()                            {}
func (m *gcTestIPAM) AllocateIPString(ipAddr, owner string) error { return nil }
func (m *gcTestIPAM) AllocateIPWithoutSyncUpstream(ip net.IP, owner string) (*ipam.AllocationResult, error) {
	return nil, nil
}
func (m *gcTestIPAM) ReleaseIPString(releaseArg string) error {
	if m.released == nil {
		m.released = make(map[string]bool)
	}
	m.released[releaseArg] = true
	return nil
}
func (m *gcTestIPAM) ReleaseIPWithOwner(ipStr string, owner string) error { return nil }
func (m *gcTestIPAM) AllocateNextWithExpiration(family, owner string, timeout time.Duration) (*ipam.AllocationResult, *ipam.AllocationResult, error) {
	return nil, nil, nil
}
func (m *gcTestIPAM) AllocateNext(family, owner string) (*ipam.AllocationResult, *ipam.AllocationResult, error) {
	return nil, nil, nil
}
func (m *gcTestIPAM) AllocateNextFamilyWithoutSyncUpstream(family ipam.Family, owner string) (*ipam.AllocationResult, error) {
	return nil, nil
}
func (m *gcTestIPAM) Dump() (map[string]string, map[string]string, string) {
	return m.allocv4, m.allocv6, m.status
}

func TestNewAgentGcer(t *testing.T) {
	ea := &EndpointAllocator{}
	gcer := newAgentGcer(ea)
	require.NotNil(t, gcer.expiredIPMap)
	require.NotNil(t, gcer.expiredCepMap)
	require.Equal(t, ea, gcer.ea)
}

func TestDynamicEndpointsGC(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	mockCfg := &gcTestConfig{gcInterval: time.Second}

	ea := &EndpointAllocator{
		c:                 mockCfg,
		cceEndpointClient: &watchers.CCEEndpointClient{},
		podClient:         &watchers.PodClient{},
	}
	gcer := newAgentGcer(ea)

	epOld := &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "ep-old", Namespace: "ns"}}
	epOld.Spec.ExternalIdentifiers = &models.EndpointIdentifiers{K8sPodName: "pod-old"}
	epNew := &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "ep-new", Namespace: "ns"}}
	epNew.Spec.ExternalIdentifiers = &models.EndpointIdentifiers{K8sPodName: "pod-new"}
	// PSTS endpoint should be skipped
	epPSTS := &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "ep-psts", Namespace: "ns"}}
	epPSTS.Spec.Network.IPAllocation = &ccev2.IPAllocation{PSTSName: "psts"}
	// RDMA endpoint should be skipped
	rdmaName := bceutils.GetCEPNameFromPodName(true, "pod-rdma", "aa:bb:cc:dd:ee:ff")
	epRDMA := &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: rdmaName, Namespace: "ns"}}

	patches.ApplyMethod(reflect.TypeOf(ea.cceEndpointClient), "List", func(_ *watchers.CCEEndpointClient) ([]*ccev2.CCEEndpoint, error) {
		return []*ccev2.CCEEndpoint{epOld, epNew, epPSTS, epRDMA}, nil
	})
	patches.ApplyMethod(reflect.TypeOf(ea.podClient), "Get", func(_ *watchers.PodClient, namespace, name string) (*corev1.Pod, error) {
		return nil, kerrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, name)
	})
	patches.ApplyFunc(isThisCEPReadyForDelete, func(_ *logrus.Entry, _ *ccev2.CCEEndpoint, _ bool) bool { return true })

	deleteCount := 0
	patches.ApplyPrivateMethod(reflect.TypeOf(ea), "tryDeleteEndpointAfterPodDeleted", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint, _ bool, _ *logrus.Entry) error {
		deleteCount++
		return nil
	})
	patches.ApplyMethod(reflect.TypeOf(ea.cceEndpointClient), "Get", func(_ *watchers.CCEEndpointClient, namespace, name string) (*ccev2.CCEEndpoint, error) {
		if name == "ep-missing" {
			return nil, kerrors.NewNotFound(schema.GroupResource{Group: "", Resource: "cceendpoints"}, name)
		}
		if name == "ep-new" {
			return epNew, nil
		}
		return nil, kerrors.NewNotFound(schema.GroupResource{Group: "", Resource: "cceendpoints"}, name)
	})

	// pre-populate expired map with old and missing
	gcer.expiredCepMap["ns/ep-old"] = time.Now().Add(-10 * time.Second)
	gcer.expiredCepMap["ns/ep-missing"] = time.Now().Add(-10 * time.Second)

	gcer.dynamicEndpointsGC()

	require.Equal(t, 1, deleteCount)
	_, hasOld := gcer.expiredCepMap["ns/ep-old"]
	require.False(t, hasOld)
	_, hasMissing := gcer.expiredCepMap["ns/ep-missing"]
	require.False(t, hasMissing)
	_, hasNew := gcer.expiredCepMap["ns/ep-new"]
	require.True(t, hasNew)
}

func TestDynamicUsedIPsGC(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	mockCfg := &gcTestConfig{gcInterval: time.Second}

	mockIPAM := &gcTestIPAM{
		allocv4: map[string]string{
			"10.0.0.1": "ns/pod1",
			"10.0.0.2": "ns/pod2-" + ipamOption.IPAMVpcRoute,
		},
		allocv6: map[string]string{},
		status:  "ok",
	}

	ea := &EndpointAllocator{
		c:                 mockCfg,
		dynamicIPAM:       mockIPAM,
		cceEndpointClient: &watchers.CCEEndpointClient{},
	}
	gcer := newAgentGcer(ea)

	// pre-populate expired map so it can be released this round
	gcer.expiredIPMap["10.0.0.1"] = &ipOwnerTime{owner: "ns/pod1", time: time.Now().Add(-10 * time.Second), lastVisted: time.Now().Add(-10 * time.Second)}
	gcer.expiredIPMap["10.0.0.2"] = &ipOwnerTime{owner: "ns/pod2-" + ipamOption.IPAMVpcRoute, time: time.Now().Add(-10 * time.Second), lastVisted: time.Now().Add(-10 * time.Second)}

	patches.ApplyMethod(reflect.TypeOf(ea.cceEndpointClient), "Get", func(_ *watchers.CCEEndpointClient, namespace, name string) (*ccev2.CCEEndpoint, error) {
		return nil, kerrors.NewNotFound(schema.GroupResource{Group: "", Resource: "cceendpoints"}, name)
	})

	gcer.dynamicUsedIPsGC()

	require.True(t, mockIPAM.released["10.0.0.1"])
	_, hasRoute := gcer.expiredIPMap["10.0.0.2"]
	require.False(t, hasRoute)
}

func TestPSTSFixedIPEndpointsGC(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	now := time.Now()
	old := now.Add(-2 * time.Second)
	oldStr := old.Format(time.RFC3339)
	ttlShort := int64(1)
	ttlLong := int64(100)

	epExpire := &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "ep-expire", Namespace: "ns"}}
	epExpire.Spec.Network.IPAllocation = &ccev2.IPAllocation{
		DirectIPAllocation: ccev2.DirectIPAllocation{
			Type:                   ccev2.IPAllocTypeFixed,
			TTLSecondsAfterDeleted: &ttlShort,
		},
		PSTSName: "psts",
	}
	epExpire.Status.State = string(models.EndpointStatePodDeleted)
	epExpire.Status.Log = []*models.EndpointStatusChange{{State: models.EndpointStatePodDeleted, Timestamp: oldStr}}

	epKeep := &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "ep-keep", Namespace: "ns"}}
	epKeep.Spec.Network.IPAllocation = &ccev2.IPAllocation{
		DirectIPAllocation: ccev2.DirectIPAllocation{
			Type:                   ccev2.IPAllocTypeFixed,
			TTLSecondsAfterDeleted: &ttlLong,
		},
		PSTSName: "psts",
	}
	epKeep.Status.State = string(models.EndpointStatePodDeleted)
	epKeep.Status.Log = []*models.EndpointStatusChange{{State: models.EndpointStatePodDeleted, Timestamp: oldStr}}

	epNoLog := &ccev2.CCEEndpoint{ObjectMeta: metav1.ObjectMeta{Name: "ep-nolog", Namespace: "ns"}}
	epNoLog.Spec.Network.IPAllocation = &ccev2.IPAllocation{
		DirectIPAllocation: ccev2.DirectIPAllocation{
			Type:                   ccev2.IPAllocTypeFixed,
			TTLSecondsAfterDeleted: &ttlShort,
		},
		PSTSName: "psts",
	}
	epNoLog.Status.State = string(models.EndpointStatePodDeleted)

	ea := &EndpointAllocator{cceEndpointClient: &watchers.CCEEndpointClient{}}
	gcer := newAgentGcer(ea)

	patches.ApplyMethod(reflect.TypeOf(ea.cceEndpointClient), "List", func(_ *watchers.CCEEndpointClient) ([]*ccev2.CCEEndpoint, error) {
		return []*ccev2.CCEEndpoint{epExpire, epKeep, epNoLog}, nil
	})

	deleteCount := 0
	patches.ApplyPrivateMethod(reflect.TypeOf(ea), "deleteEndpointSafely", func(_ *EndpointAllocator, _ *ccev2.CCEEndpoint) error {
		deleteCount++
		return nil
	})

	gcer.pstsFixedIPEndpointsGC()

	require.Equal(t, 1, deleteCount)
}
