//go:build linux && amd64

package agent

import (
	"encoding/binary"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

func TestOnUpdateENI_StatusDeepEqual_NoUpdateStatus(t *testing.T) {
	unpatch, err := patchFunc((*eniLink).ensureENINeigh, func(*eniLink) error { return nil })
	if err != nil {
		t.Fatalf("patch ensureENINeigh: %v", err)
	}
	t.Cleanup(unpatch)

	linkName, linkIndex, macStr := mustPickExistingLinkWithMAC(t)

	eniID := "eni-ut-deepequal-no-update"
	newENI := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:          eniID,
				Description: defaults.DefaultENIDescription,
				MacAddress:  macStr,
			},
			UseMode: ccev2.ENIUseModePrimaryWithSecondaryIP,
			Type:    ccev2.ENIForEBC,
		},
		Status: ccev2.ENIStatus{
			VPCStatus:      ccev2.VPCENIStatusInuse,
			CCEStatus:      ccev2.ENIStatusReadyOnNode,
			InterfaceIndex: linkIndex,
			InterfaceName:  linkName,
			ENIIndex:       0,
		},
	}
	oldENI := newENI.DeepCopy()

	factory := &eniInitFactory{
		fullENIs:  make(map[string]*ccev2.ENI),
		localENIs: make(map[string]*ccev2.ENI),
		eniClient: nil,
		release:   nil,
	}

	assert.NotPanics(t, func() {
		err := factory.OnUpdateENI(oldENI, newENI)
		assert.NoError(t, err)
	})
}

func mustPickExistingLinkWithMAC(t *testing.T) (linkName string, linkIndex int, macStr string) {
	t.Helper()

	links, err := netlink.LinkList()
	if err != nil {
		t.Fatalf("netlink.LinkList: %v", err)
	}
	for _, l := range links {
		attrs := l.Attrs()
		if attrs == nil || attrs.Index <= 0 || attrs.Name == "" {
			continue
		}
		if len(attrs.HardwareAddr) == 0 || attrs.Name == "lo" {
			continue
		}
		return attrs.Name, attrs.Index, attrs.HardwareAddr.String()
	}
	t.Skip("no non-loopback link with MAC found")
	return "", 0, ""
}

// patchFunc patches a function by rewriting its prologue to jump to replacement.
// linux/amd64 only, test-only usage.
func patchFunc(target any, replacement any) (unpatch func(), err error) {
	targetPC := reflect.ValueOf(target).Pointer()
	replacementPC := reflect.ValueOf(replacement).Pointer()

	const patchSize = 12 // movabs rax, imm64; jmp rax
	orig := make([]byte, patchSize)
	copy(orig, unsafe.Slice((*byte)(unsafe.Pointer(targetPC)), patchSize))

	pageSize := syscall.Getpagesize()
	pageStart := targetPC & ^(uintptr(pageSize - 1))
	page := unsafe.Slice((*byte)(unsafe.Pointer(pageStart)), pageSize)
	if err := syscall.Mprotect(page, syscall.PROT_READ|syscall.PROT_WRITE|syscall.PROT_EXEC); err != nil {
		return nil, err
	}

	patch := make([]byte, patchSize)
	patch[0] = 0x48
	patch[1] = 0xB8
	binary.LittleEndian.PutUint64(patch[2:10], uint64(replacementPC))
	patch[10] = 0xFF
	patch[11] = 0xE0

	copy(unsafe.Slice((*byte)(unsafe.Pointer(targetPC)), patchSize), patch)
	runtime.KeepAlive(target)
	runtime.KeepAlive(replacement)

	return func() {
		_ = syscall.Mprotect(page, syscall.PROT_READ|syscall.PROT_WRITE|syscall.PROT_EXEC)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(targetPC)), patchSize), orig)
		_ = syscall.Mprotect(page, syscall.PROT_READ|syscall.PROT_EXEC)
	}, nil
}
