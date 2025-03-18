package main

import (
	"fmt"
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
)

type MockNetworkChecker struct {
	mock.Mock
}

func (m *MockNetworkChecker) GetDefaultGateway() (net.IP, error) {
	args := m.Called()
	ip := args.Get(0)
	if ip == nil {
		return nil, args.Error(1)
	}
	return ip.(net.IP), args.Error(1)
}

func (m *MockNetworkChecker) SendIcmpProbeInNetNS(netns ns.NetNS, srcIP, targetIP net.IP, mtu int) (bool, error) {
	args := m.Called(netns, srcIP, targetIP, mtu)
	return args.Bool(0), args.Error(1)
}

var _ NetworkChecker = &MockNetworkChecker{}

var _ = Describe("VerifyNetworkConnectivity", func() {
	var originalNS, targetNS ns.NetNS
	var mockUtils *MockNetworkUtils
	logger = logging.DefaultLogger.WithFields(logrus.Fields{
		"cmdArgs": logfields.Json("test"),
		"plugin":  "cptp",
		"mod":     "ADD",
	})

	BeforeEach(func() {
		var err error
		originalNS, err = testutils.NewNS()
		Expect(err).NotTo(HaveOccurred())
		targetNS, err = testutils.NewNS()
		Expect(err).NotTo(HaveOccurred())

		mockUtils = &MockNetworkUtils{}
	})

	AfterEach(func() {
		Expect(originalNS.Close()).To(Succeed())
		Expect(testutils.UnmountNS(originalNS)).To(Succeed())
		Expect(targetNS.Close()).To(Succeed())
		Expect(testutils.UnmountNS(targetNS)).To(Succeed())
	})

	It("should verify network connectivity for IPv4 addresses", func() {
		result := &types100.Result{
			IPs: []*types100.IPConfig{
				{
					Address: net.IPNet{
						IP:   net.ParseIP("192.168.1.10"),
						Mask: net.CIDRMask(24, 32),
					},
					Gateway: net.ParseIP("192.168.1.1"),
				},
			},
		}

		mockUtils.On("SendIcmpProbeInNetNS", targetNS, net.ParseIP("192.168.1.10"), net.ParseIP("192.168.1.1"), 1500).Return(true, nil)

		err := targetNS.Do(func(ns.NetNS) error {
			defer GinkgoRecover()

			err := VerifyNetworkConnectivity(result, targetNS, 1500, mockUtils)
			Expect(err).NotTo(HaveOccurred())
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should skip non-IPv4 addresses", func() {
		result := &types100.Result{
			IPs: []*types100.IPConfig{
				{
					Address: net.IPNet{
						IP:   net.ParseIP("2001:db8::1"),
						Mask: net.CIDRMask(64, 128),
					},
					Gateway: net.ParseIP("2001:db8::1"),
				},
			},
		}

		err := targetNS.Do(func(ns.NetNS) error {
			defer GinkgoRecover()

			err := VerifyNetworkConnectivity(result, targetNS, 1500, mockUtils)
			Expect(err).NotTo(HaveOccurred())
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error if failed to get default gateway for vpc route mode", func() {
		result := &types100.Result{
			IPs: []*types100.IPConfig{
				{
					Address: net.IPNet{
						IP:   net.ParseIP("192.168.1.10"),
						Mask: net.CIDRMask(32, 32),
					},
					Gateway: nil,
				},
			},
		}

		mockUtils.On("GetDefaultGateway").Return(nil, fmt.Errorf("failed to get default gateway"))

		err := targetNS.Do(func(ns.NetNS) error {
			defer GinkgoRecover()

			err := VerifyNetworkConnectivity(result, targetNS, 1500, mockUtils)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get default gateway for vpc route mode"))
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error if failed to verify network connectivity", func() {
		result := &types100.Result{
			IPs: []*types100.IPConfig{
				{
					Address: net.IPNet{
						IP:   net.ParseIP("192.168.1.10"),
						Mask: net.CIDRMask(24, 32),
					},
					Gateway: net.ParseIP("192.168.1.1"),
				},
			},
		}

		mockUtils.On("SendIcmpProbeInNetNS", targetNS, net.ParseIP("192.168.1.10"), net.ParseIP("192.168.1.1"), 1500).Return(false, fmt.Errorf("failed to send ICMP probe"))

		err := targetNS.Do(func(ns.NetNS) error {
			defer GinkgoRecover()

			err := VerifyNetworkConnectivity(result, targetNS, 1500, mockUtils)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to verify network connectivity"))
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error if failed to ping gateway", func() {
		result := &types100.Result{
			IPs: []*types100.IPConfig{
				{
					Address: net.IPNet{
						IP:   net.ParseIP("192.168.1.10"),
						Mask: net.CIDRMask(24, 32),
					},
					Gateway: net.ParseIP("192.168.1.1"),
				},
			},
		}

		mockUtils.On("SendIcmpProbeInNetNS", targetNS, net.ParseIP("192.168.1.10"), net.ParseIP("192.168.1.1"), 1500).Return(false, nil)

		err := targetNS.Do(func(ns.NetNS) error {
			defer GinkgoRecover()

			err := VerifyNetworkConnectivity(result, targetNS, 1500, mockUtils)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to ping gateway"))
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
