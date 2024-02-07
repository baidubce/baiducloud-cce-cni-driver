package nlmonitor

import (
	"syscall"

	"github.com/vishvananda/netlink"
)

type LinkMonitorHandler interface {
	HandleNewlink(link netlink.Link)
	HandleDellink(link netlink.Link)
}

// LinkMonitor keeps track of the interfaces on this host. It can invoke a
// callback when an interface is added/deleted.
type LinkMonitor struct {
	// receive LinkUpdates from nl.Subscribe
	updates chan netlink.LinkUpdate
	// close(nlDone) to terminate Subscribe loop
	done chan struct{}
	// handle events when link added or deleted
	handler LinkMonitorHandler
}

func NewLinkMonitor(handler LinkMonitorHandler) (*LinkMonitor, error) {
	nlmon := &LinkMonitor{
		updates: make(chan netlink.LinkUpdate),
		done:    make(chan struct{}),
		handler: handler,
	}
	err := netlink.LinkSubscribe(nlmon.updates, nlmon.done)
	defer func() {
		if err != nil {
			nlmon.Close()
		}
	}()
	if err != nil {
		return nil, err
	}
	go nlmon.ParseLinkUpdates()

	return nlmon, nil
}

func (lm *LinkMonitor) ParseLinkUpdates() {
	for {
		select {
		case evt, ok := <-lm.updates:
			if !ok {
				return
			}

			// ifi_change indicates link state change. 0xFFFFFFFF means link is added or deleted.
			// Ref: https://github.com/torvalds/linux/blob/8ab774587903771821b59471cc723bba6d893942/net/core/rtnetlink.c#L3141
			// ~0U == 0xFFFFFFFF
			if evt.IfInfomsg.Change != 0xFFFFFFFF {
				break
			}

			switch evt.Header.Type {
			case syscall.RTM_NEWLINK:
				lm.handler.HandleNewlink(evt.Link)
			case syscall.RTM_DELLINK:
				lm.handler.HandleDellink(evt.Link)
			}
		}
	}
}

func (lm *LinkMonitor) Close() {
	close(lm.done)
}
