package link

import (
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

type ContainerContext struct {
	ID             string
	NetnsPath      string
	Driver         string
	ContainerDev   netlink.Link
	HostDev        netlink.Link
	ContainerNetns ns.NetNS
}

func NewContainerContext(id string, netnsPath string) (*ContainerContext, error) {
	var err error

	ctx := &ContainerContext{ID: id, NetnsPath: netnsPath}
	ctx.ContainerNetns, err = ns.GetNS(ctx.NetnsPath)
	if err != nil {
		return ctx, fmt.Errorf("failed to open netns %q: %v", ctx.NetnsPath, err)
	}
	links, err := ctx.GetContainerDev("eth0")
	if len(links) > 0 {
		ctx.ContainerDev = links[0]
	}
	if len(links) > 1 {
		ctx.HostDev = links[1]
	}
	return ctx, err
}

func (ctx *ContainerContext) GetContainerDev(devName string) ([]netlink.Link, error) {
	var (
		err   error
		links []netlink.Link
	)

	err = ctx.ContainerNetns.Do(func(_ ns.NetNS) error {
		dev, err := netlink.LinkByName(devName)
		if err != nil {
			return fmt.Errorf("failed to find container device %q: %v", devName, err)
		}
		links = append(links, dev)
		return nil
	})
	if err != nil {
		return links, fmt.Errorf("failed to get container device %q: %v", devName, err)
	}

	ctx.Driver = links[0].Type()
	switch ctx.Driver {
	case string(models.DatapathModeVeth), string(models.DatapathModeIpvlan):
		peerIndex := links[0].Attrs().ParentIndex
		dev, err := netlink.LinkByIndex(peerIndex)
		if err != nil {
			return links, fmt.Errorf("failed to find host device with index %d: %v", peerIndex, err)
		}
		links = append(links, dev)
	}
	return links, nil
}

func (ctx *ContainerContext) Close() {
	if ctx.ContainerNetns != nil {
		_ = ctx.ContainerNetns.Close()
	}
}
