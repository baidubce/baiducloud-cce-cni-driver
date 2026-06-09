package endpoint

import (
	"errors"
	"io"
	"net"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiModels "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/common"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	mock_netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/netlinkwrapper/mocks"
)

// mockLink is a mock implementation of netlink.Link
type mockLink struct {
	attrs *netlink.LinkAttrs
}

func (m *mockLink) Attrs() *netlink.LinkAttrs {
	return m.attrs
}

func (m *mockLink) Type() string {
	return "mock"
}

func TestGetIpFromLink(t *testing.T) {
	link := &mockLink{
		attrs: &netlink.LinkAttrs{
			Index: 1,
			Name:  "eth0",
		},
	}
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	// init mockNetlink from mock_netlinkwrapper
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(ctrl)

	ip := net.ParseIP("192.168.1.1")
	ipNet := &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(24, 32),
	}
	addr := &netlink.Addr{IPNet: ipNet}
	mockNetlink.EXPECT().AddrAdd(link, addr).Return(nil).MaxTimes(1)
	err := mockNetlink.AddrAdd(link, addr)
	if err != nil {
		t.Fatalf("Failed to add address to mock link: %v", err)
	}
	addrs := []netlink.Addr{
		{
			IPNet: ipNet,
			Label: "eth0",
		},
	}
	mockNetlink.EXPECT().AddrList(link, netlink.FAMILY_V4).Return(addrs, nil).MaxTimes(2)
	found, err := getIpFromLink(mockNetlink, link, netlink.FAMILY_V4, "192.168.1.1")
	if err != nil {
		t.Fatalf("getIpFromLink returned an error: %v", err)
	}
	if !found {
		t.Error("Expected IP to be found, but it was not")
	}

	found, err = getIpFromLink(mockNetlink, link, netlink.FAMILY_V4, "192.168.1.2")
	if err != nil {
		t.Fatalf("getIpFromLink returned an error: %v", err)
	}
	if found {
		t.Error("Expected IP not to be found, but it was")
	}

	mockNetlink.EXPECT().AddrList(link, netlink.FAMILY_V6).Return(nil, errors.New("mock error: cat not get address")).MaxTimes(1)
	_, err = getIpFromLink(mockNetlink, link, netlink.FAMILY_V6, "192.168.1.1")
	if err == nil {
		t.Fatalf("getIpFromLink not returned an error when it should have")
	}
}

func TestIsThisCEPReadyForDelete_GivenNilNetworking_WhenChecked_ThenReturnTrue(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	ep.Status.Networking = nil
	logEntry := newTestLogEntry()

	if !isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be ready when networking is nil")
	}
}

func TestIsThisCEPReadyForDelete_GivenNilStatusExternalIdentifiers_WhenChecked_ThenReturnTrue(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	ep.Status.ExternalIdentifiers = nil
	logEntry := newTestLogEntry()

	if !isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be ready when status external identifiers are nil")
	}
}

func TestIsThisCEPReadyForDelete_GivenEmptyPodIdentifiers_WhenChecked_ThenReturnTrue(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	ep.Namespace = ""
	ep.Name = ""
	ep.Spec.ExternalIdentifiers.K8sPodName = ""
	logEntry := newTestLogEntry()

	if !isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be ready when pod identifiers are empty")
	}
}

func TestIsThisCEPReadyForDelete_GivenNoVethFound_WhenChecked_ThenReturnTrue(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	ep.Spec.ExternalIdentifiers.ContainerID = ""
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	vethName := common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth")
	mockNetlink.EXPECT().LinkByName(vethName).Return(nil, errors.New("not found"))

	if !isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be ready when veth link is missing")
	}
}

func TestIsThisCEPReadyForDelete_GivenContainerIDVethFound_WhenChecked_ThenUseContainerIDVeth(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	ep.Spec.ExternalIdentifiers.ContainerID = "container-123"
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	// veth name based on pod name
	vethName1 := common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth")
	// veth name based on containerID
	vethName2 := common.VethNameForPod(ep.Spec.ExternalIdentifiers.ContainerID, ep.Namespace, "veth")

	// containerID-based veth exists (preferred)
	link2 := newTestLink(vethName2, 10)
	mockNetlink.EXPECT().LinkByName(vethName1).Return(nil, errors.New("not found"))
	mockNetlink.EXPECT().LinkByName(vethName2).Return(link2, nil)
	mockNetlink.EXPECT().RouteList(gomock.Any(), netlink.FAMILY_V4).Return([]netlink.Route{}, nil)

	if !isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be ready when no routes exist")
	}
}

func TestIsThisCEPReadyForDelete_GivenBothVethFound_WhenChecked_ThenPreferContainerIDVeth(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	ep.Spec.ExternalIdentifiers.ContainerID = "container-123"
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	// veth name based on pod name
	vethName1 := common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth")
	// veth name based on containerID
	vethName2 := common.VethNameForPod(ep.Spec.ExternalIdentifiers.ContainerID, ep.Namespace, "veth")

	// both veth exist, but containerID-based veth should be preferred
	link1 := newTestLink(vethName1, 5)
	link2 := newTestLink(vethName2, 10)
	mockNetlink.EXPECT().LinkByName(vethName1).Return(link1, nil)
	mockNetlink.EXPECT().LinkByName(vethName2).Return(link2, nil)
	// should use link2 (containerID-based) for route check
	mockNetlink.EXPECT().RouteList(gomock.Any(), netlink.FAMILY_V4).Return([]netlink.Route{}, nil)

	if !isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be ready when no routes exist")
	}
}

func TestIsThisCEPReadyForDelete_GivenRouteListError_WhenChecked_ThenReturnFalse(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	link := newTestLink(common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth"), 5)
	mockNetlink.EXPECT().LinkByName(link.Attrs().Name).Return(link, nil)
	mockNetlink.EXPECT().RouteList(gomock.Any(), netlink.FAMILY_V4).Return(nil, errors.New("route err"))

	if isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be not ready when route list fails")
	}
}

func TestIsThisCEPReadyForDelete_GivenNoRoutesFound_WhenChecked_ThenReturnTrue(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	link := newTestLink(common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth"), 6)
	mockNetlink.EXPECT().LinkByName(link.Attrs().Name).Return(link, nil)
	mockNetlink.EXPECT().RouteList(gomock.Any(), netlink.FAMILY_V4).Return([]netlink.Route{}, nil)

	if !isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be ready when no routes exist")
	}
}

func TestIsThisCEPReadyForDelete_GivenRoutesExistAndNoCleanup_WhenChecked_ThenReturnFalse(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	link := newTestLink(common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth"), 7)
	mockNetlink.EXPECT().LinkByName(link.Attrs().Name).Return(link, nil)
	mockNetlink.EXPECT().RouteList(gomock.Any(), netlink.FAMILY_V4).Return([]netlink.Route{newRoute("10.0.0.1", link.Attrs().Index)}, nil)

	if isThisCEPReadyForDelete(logEntry, ep, false) {
		t.Fatalf("expected endpoint to be not ready when routes exist and cleanup is disabled")
	}
}

func TestIsThisCEPReadyForDelete_GivenRoutesExistAndCleanupSucceeds_WhenChecked_ThenReturnTrue(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	link := newTestLink(common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth"), 8)
	mockNetlink.EXPECT().LinkByName(link.Attrs().Name).Return(link, nil)
	mockNetlink.EXPECT().RouteList(gomock.Any(), netlink.FAMILY_V4).Return([]netlink.Route{newRoute("10.0.0.1", link.Attrs().Index)}, nil)
	mockNetlink.EXPECT().LinkDel(link).Return(nil)

	if !isThisCEPReadyForDelete(logEntry, ep, true) {
		t.Fatalf("expected endpoint to be ready when cleanup succeeds")
	}
}

func TestIsThisCEPReadyForDelete_GivenRoutesExistAndCleanupFails_WhenChecked_ThenReturnFalse(t *testing.T) {
	ep := newTestEndpoint("10.0.0.1")
	mockNetlink := setupMockNetlink(t)
	logEntry := newTestLogEntry()

	link := newTestLink(common.VethNameForPod(ep.Spec.ExternalIdentifiers.K8sPodName, ep.Namespace, "veth"), 9)
	mockNetlink.EXPECT().LinkByName(link.Attrs().Name).Return(link, nil)
	mockNetlink.EXPECT().RouteList(gomock.Any(), netlink.FAMILY_V4).Return([]netlink.Route{newRoute("10.0.0.1", link.Attrs().Index)}, nil)
	mockNetlink.EXPECT().LinkDel(link).Return(errors.New("delete failed"))

	if isThisCEPReadyForDelete(logEntry, ep, true) {
		t.Fatalf("expected endpoint to be not ready when cleanup fails")
	}
}

func newTestEndpoint(ip string) *ccev2.CCEEndpoint {
	return &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test-cep",
		},
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &apiModels.EndpointIdentifiers{
				K8sPodName:  "test-pod",
				ContainerID: "",
			},
		},
		Status: ccev2.EndpointStatus{
			ExternalIdentifiers: &apiModels.EndpointIdentifiers{},
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					{
						IP:     ip,
						Family: ccev2.IPFamily("4"),
					},
				},
			},
		},
	}
}

func newTestLink(name string, index int) netlink.Link {
	return &mockLink{
		attrs: &netlink.LinkAttrs{
			Name:  name,
			Index: index,
		},
	}
}

func newRoute(ip string, linkIndex int) netlink.Route {
	return netlink.Route{
		Table:     0,
		LinkIndex: linkIndex,
		Dst: &net.IPNet{
			IP:   net.ParseIP(ip),
			Mask: net.CIDRMask(32, 32),
		},
	}
}

func newTestLogEntry() *logrus.Entry {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logrus.NewEntry(logger)
}

func setupMockNetlink(t *testing.T) *mock_netlinkwrapper.MockNetLink {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(ctrl)
	original := netlinkImpl
	netlinkImpl = mockNetlink
	t.Cleanup(func() {
		netlinkImpl = original
		ctrl.Finish()
	})
	return mockNetlink
}

// TestCheckRoutesExistInMainTable tests the checkRoutesExistInMainTable function
func TestCheckRoutesExistInMainTable(t *testing.T) {
	type TestFields struct {
		mockNetlink *mock_netlinkwrapper.MockNetLink
	}

	type TestArgs struct {
		ips       []net.IP
		linkIndex int
	}

	type TestCase struct {
		Name        string
		TestSetup   func() *TestFields
		TestPostRun func(fields *TestFields)
		Args        TestArgs
		WantResult  bool
		WantErr     error
	}

	cases := []TestCase{
		{
			Name: "TestCheckRoutesExistInMainTable_GivenEmptyIPs_WhenCalled_ThenReturnFalse",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// No expectations needed for empty IPs
			},
			Args: TestArgs{
				ips:       []net.IP{},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv4IPWithRoute_WhenCalled_ThenReturnTrue",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				ip := net.ParseIP("192.168.1.1")
				routes := []netlink.Route{
					{
						Table:     0,
						LinkIndex: 10,
						Dst: &net.IPNet{
							IP:   ip,
							Mask: net.CIDRMask(32, 32),
						},
					},
				}
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V4).Return(routes, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("192.168.1.1")},
				linkIndex: 10,
			},
			WantResult: true,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv4RouteListError_WhenCalled_ThenReturnError",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V4).Return(nil, errors.New("route list failed"))
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("192.168.1.1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    errors.New("failed to list IPv4 routes: route list failed"),
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv4IPWithoutRoute_WhenCalled_ThenReturnFalse",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V4).Return([]netlink.Route{}, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("192.168.1.1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenMixedIPv4AndIPv6_WhenBothNoRoute_WhenCalled_ThenReturnFalse",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				// IPv4 route list returns empty
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V4).Return([]netlink.Route{}, nil)
				// IPv6 route list returns empty
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return([]netlink.Route{}, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("192.168.1.1"), net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenMixedIPv4AndIPv6_WhenIPv4RouteListError_WhenCalled_ThenReturnError",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				// IPv4 route list returns error, should not check IPv6
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V4).Return(nil, errors.New("route list failed"))
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("192.168.1.1"), net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    errors.New("failed to list IPv4 routes: route list failed"),
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv6IPWithRoute_WhenCalled_ThenReturnTrue",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				ip := net.ParseIP("2001:db8::1")
				routes := []netlink.Route{
					{
						Table:     0,
						LinkIndex: 10,
						Dst: &net.IPNet{
							IP:   ip,
							Mask: net.CIDRMask(128, 128),
						},
					},
				}
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return(routes, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: true,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv6IPWithoutRoute_WhenCalled_ThenReturnFalse",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return([]netlink.Route{}, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv6RouteListError_WhenCalled_ThenReturnError",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return(nil, errors.New("route list failed"))
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    errors.New("failed to list IPv6 routes: route list failed"),
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenMixedIPv4AndIPv6_WhenIPv4NoRouteButIPv6HasRoute_WhenCalled_ThenReturnTrue",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				ipv6 := net.ParseIP("2001:db8::1")
				// IPv4 route list returns empty
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V4).Return([]netlink.Route{}, nil)
				// IPv6 route list returns route
				routes := []netlink.Route{
					{
						Table:     0,
						LinkIndex: 10,
						Dst: &net.IPNet{
							IP:   ipv6,
							Mask: net.CIDRMask(128, 128),
						},
					},
				}
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return(routes, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("192.168.1.1"), net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: true,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenMultipleIPv6IPs_WhenOneHasRoute_WhenCalled_ThenReturnTrue",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				ip1 := net.ParseIP("2001:db8::1")
				routes := []netlink.Route{
					{
						Table:     0,
						LinkIndex: 10,
						Dst: &net.IPNet{
							IP:   ip1,
							Mask: net.CIDRMask(128, 128),
						},
					},
				}
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return(routes, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")},
				linkIndex: 10,
			},
			WantResult: true,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv6IPWithDifferentLinkIndex_WhenCalled_ThenReturnFalse",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				ip := net.ParseIP("2001:db8::1")
				routes := []netlink.Route{
					{
						Table:     0,
						LinkIndex: 20, // Different link index
						Dst: &net.IPNet{
							IP:   ip,
							Mask: net.CIDRMask(128, 128),
						},
					},
				}
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return(routes, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenIPv6IPWithNonZeroTable_WhenCalled_ThenReturnFalse",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				ip := net.ParseIP("2001:db8::1")
				routes := []netlink.Route{
					{
						Table:     100, // Non-zero table (not main table)
						LinkIndex: 10,
						Dst: &net.IPNet{
							IP:   ip,
							Mask: net.CIDRMask(128, 128),
						},
					},
				}
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return(routes, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: false,
			WantErr:    nil,
		},
		{
			Name: "TestCheckRoutesExistInMainTable_GivenNilIP_WhenCalled_ThenSkipNilIP",
			TestSetup: func() *TestFields {
				mockNetlink := setupMockNetlink(t)
				ipv6 := net.ParseIP("2001:db8::1")
				routes := []netlink.Route{
					{
						Table:     0,
						LinkIndex: 10,
						Dst: &net.IPNet{
							IP:   ipv6,
							Mask: net.CIDRMask(128, 128),
						},
					},
				}
				mockNetlink.EXPECT().RouteList(nil, netlink.FAMILY_V6).Return(routes, nil)
				return &TestFields{
					mockNetlink: mockNetlink,
				}
			},
			TestPostRun: func(fields *TestFields) {
				// Expectations verified by gomock
			},
			Args: TestArgs{
				ips:       []net.IP{nil, net.ParseIP("2001:db8::1")},
				linkIndex: 10,
			},
			WantResult: true,
			WantErr:    nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fields := tc.TestSetup()
			gotResult, gotErr := checkRoutesExistInMainTable(fields.mockNetlink, tc.Args.ips, tc.Args.linkIndex)
			if (gotErr == nil) != (tc.WantErr == nil) {
				t.Errorf("got err %v, want %v", gotErr, tc.WantErr)
			} else if gotErr != nil && tc.WantErr != nil && gotErr.Error() != tc.WantErr.Error() {
				t.Errorf("got err %v, want %v", gotErr, tc.WantErr)
			}
			if gotResult != tc.WantResult {
				t.Errorf("got result %v, want %v", gotResult, tc.WantResult)
			}
			if tc.TestPostRun != nil {
				tc.TestPostRun(fields)
			}
		})
	}
}
