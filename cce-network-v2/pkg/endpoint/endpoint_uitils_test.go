package endpoint

import (
	"errors"
	"net"
	"testing"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	mock_netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/netlinkwrapper/mocks"
	mock_nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/nswrapper/mocks"
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

func TestIsThisCEPReadyForDelete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	// init mockNetlink from mock_netlinkwrapper
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(ctrl)
	// init netlinkImpl from mock_netlinkwrapper for testing
	netlinkImpl = mockNetlink
	// init mockNS from mock_nswrapper
	mockNS := mock_nswrapper.NewMockNS(ctrl)
	// init nsImpl from mock_nswrapper for testing
	nsImpl = mockNS

	// Setup
	scopedLog := logging.DefaultLogger.WithField(logfields.LogSubsys, allocatorComponentName).WithFields(logrus.Fields{
		"ip":    "testIP",
		"owner": "testOwner",
		"step":  "testisThisCEPReadyForDelete",
	})

	// Test case 1: ep.Status.Networking is nil
	ep1 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: nil,
		},
	}
	if !isThisCEPReadyForDelete(scopedLog, ep1) {
		t.Error("Expected isThisCEPReadyForDelete to return true when ep.Status.Networking is nil")
	}

	// Test case 2: ep.Status.ExternalIdentifiers is nil
	ep2 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{},
		},
	}
	if !isThisCEPReadyForDelete(scopedLog, ep2) {
		t.Error("Expected isThisCEPReadyForDelete to return true when ep.Status.ExternalIdentifiers is nil")
	}

	// Test case 3: netnsPath is empty
	ep3 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "",
			},
		},
	}
	if !isThisCEPReadyForDelete(scopedLog, ep3) {
		t.Error("Expected isThisCEPReadyForDelete to return true when netnsPath is empty")
	}

	// Test case 4: netnsPath does not start with /var/run/netns
	ep4 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/invalid/path",
			},
		},
	}
	if !isThisCEPReadyForDelete(scopedLog, ep4) {
		t.Error("Expected isThisCEPReadyForDelete to return true when netnsPath does not start with /var/run/netns")
	}

	// Test case 5: netns does not exist
	ep5 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/var/run/netns/nonexistent",
			},
		},
	}
	if !isThisCEPReadyForDelete(scopedLog, ep5) {
		t.Error("Expected isThisCEPReadyForDelete to return true when netns does not exist")
	}

	// Test case 6: Link does not exist
	ep6 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{
						Interface: "eth0",
						IP:        "192.168.1.1",
						Family:    "4",
					},
				},
			},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/var/run/netns/existing",
			},
		},
	}
	mockNS.EXPECT().WithNetNSPath("/var/run/netns/existing", gomock.Any()).
		DoAndReturn(func(_ string, f func(netns ns.NetNS) error) error {
			// give any netns (do not really use) for call to WithNetNSPath for testing
			netns, _ := ns.GetCurrentNS()
			mockNetlink.EXPECT().LinkByName("eth0").Return(nil, errors.New("mock error: no such link")).MaxTimes(1)
			err := f(netns)
			return err
		})
	if !isThisCEPReadyForDelete(scopedLog, ep6) {
		t.Error("Expected isThisCEPReadyForDelete to return false when link does not exist")
	}

	// Test case 7: IPv4 exists in netns
	ep7 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{
						Interface: "eth0",
						IP:        "192.168.1.1",
						Family:    "4",
					},
				},
			},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/var/run/netns/existing",
			},
		},
	}
	mockNS.EXPECT().WithNetNSPath("/var/run/netns/existing", gomock.Any()).
		DoAndReturn(func(_ string, f func(netns ns.NetNS) error) error {
			link := &mockLink{
				attrs: &netlink.LinkAttrs{
					Index: 1,
					Name:  "eth0",
				},
			}
			ip := net.ParseIP("192.168.1.1")
			ipNet := &net.IPNet{
				IP:   ip,
				Mask: net.CIDRMask(24, 32),
			}
			addr := &netlink.Addr{IPNet: ipNet}
			addrs := []netlink.Addr{
				{
					IPNet: ipNet,
					Label: "eth0",
				},
			}
			mockNetlink.EXPECT().AddrAdd(link, addr).Return(nil).MaxTimes(1)
			err := mockNetlink.AddrAdd(link, addr)
			if err != nil {
				t.Fatalf("Failed to add address to mock link: %v", err)
			}
			// give any netns (do not really use) for call to WithNetNSPath for testing
			netns, _ := ns.GetCurrentNS()
			mockNetlink.EXPECT().LinkByName("eth0").Return(link, nil).MaxTimes(1)
			mockNetlink.EXPECT().AddrList(link, netlink.FAMILY_V4).Return(addrs, nil).MaxTimes(1)
			err = f(netns)
			return err
		})
	if isThisCEPReadyForDelete(scopedLog, ep7) {
		t.Error("Expected isThisCEPReadyForDelete to return false when IP exists in netns")
	}

	// Test case 8: IPv4 does not exist in netns
	ep8 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{
						Interface: "eth0",
						IP:        "192.168.1.2",
						Family:    "4",
					},
				},
			},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/var/run/netns/existing",
			},
		},
	}
	mockNS.EXPECT().WithNetNSPath("/var/run/netns/existing", gomock.Any()).Return(nil)
	if !isThisCEPReadyForDelete(scopedLog, ep8) {
		t.Error("Expected isThisCEPReadyForDelete to return true when IP does not exist in netns")
	}

	// Test case 9: IPv6 exists in netns
	ep9 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{
						Interface: "eth0",
						IP:        "fd00::1",
						Family:    "6",
					},
				},
			},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/var/run/netns/existing",
			},
		},
	}
	mockNS.EXPECT().WithNetNSPath("/var/run/netns/existing", gomock.Any()).
		DoAndReturn(func(_ string, f func(netns ns.NetNS) error) error {
			link := &mockLink{
				attrs: &netlink.LinkAttrs{
					Index: 1,
					Name:  "eth0",
				},
			}
			ip := net.ParseIP("fd00::1")
			ipNet := &net.IPNet{
				IP:   ip,
				Mask: net.CIDRMask(64, 128),
			}
			addr := &netlink.Addr{IPNet: ipNet}
			addrs := []netlink.Addr{
				{
					IPNet: ipNet,
					Label: "eth0",
				},
			}
			mockNetlink.EXPECT().AddrAdd(link, addr).Return(nil).MaxTimes(1)
			err := mockNetlink.AddrAdd(link, addr)
			if err != nil {
				t.Fatalf("Failed to add address to mock link: %v", err)
			}
			// give any netns (do not really use) for call to WithNetNSPath for testing
			netns, _ := ns.GetCurrentNS()
			mockNetlink.EXPECT().LinkByName("eth0").Return(link, nil).MaxTimes(1)
			mockNetlink.EXPECT().AddrList(link, netlink.FAMILY_V6).Return(addrs, nil).MaxTimes(1)
			err = f(netns)
			return err
		})
	if isThisCEPReadyForDelete(scopedLog, ep9) {
		t.Error("Expected isThisCEPReadyForDelete to return false when IP exists in netns")
	}

	// Test case 10: IP family is not valid
	ep10 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{
						Interface: "eth0",
						IP:        "192.168.1.1",
						Family:    "invalid-ip-family",
					},
				},
			},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/var/run/netns/existing",
			},
		},
	}
	if !isThisCEPReadyForDelete(scopedLog, ep10) {
		t.Error("Expected isThisCEPReadyForDelete to return false when IP is not valid")
	}

	// Test case 11: IP is empty
	ep11 := &ccev2.CCEEndpoint{
		Status: ccev2.EndpointStatus{
			Networking: &ccev2.EndpointNetworking{
				Addressing: ccev2.AddressPairList{
					&ccev2.AddressPair{
						Interface: "eth0",
						IP:        "",
						Family:    "4",
					},
				},
			},
			ExternalIdentifiers: &models.EndpointIdentifiers{
				Netns: "/var/run/netns/existing",
			},
		},
	}
	if !isThisCEPReadyForDelete(scopedLog, ep11) {
		t.Error("Expected isThisCEPReadyForDelete to return false when IP is empty")
	}
}
