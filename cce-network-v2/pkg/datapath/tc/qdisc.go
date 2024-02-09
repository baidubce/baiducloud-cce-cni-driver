package tc

import (
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/vishvananda/netlink"
)

const (
	tcSubsys = "tc"
)

var (
	tcLog = logging.NewSubysLogger(tcSubsys)
)

func QdiscReplace(qdisc netlink.Qdisc) error {
	cmd := fmt.Sprintf("tc qdisc replace %s", qdisc.Attrs().String())
	tcLog.Infof(cmd)
	err := netlink.QdiscReplace(qdisc)
	if err != nil {
		return fmt.Errorf("error %s, %w", cmd, err)
	}
	return nil
}
func QdiscDel(qdisc netlink.Qdisc) error {
	cmd := fmt.Sprintf("tc qdisc del %s", qdisc.Attrs().String())
	tcLog.Infof(cmd)
	err := netlink.QdiscDel(qdisc)
	if err != nil {
		return fmt.Errorf("error %s, %w", cmd, err)
	}
	return nil
}

// EnsureMQQdisc write qdisc
func EnsureMQQdisc(link netlink.Link) error {
	qds, err := netlink.QdiscList(link)
	if err != nil {
		return fmt.Errorf("list qdisc for dev %s error, %w", link.Attrs().Name, err)
	}
	var prev netlink.Qdisc
	for _, q := range qds {
		_, minor := netlink.MajorMinor(q.Attrs().Handle)
		if q.Attrs().Parent == netlink.HANDLE_ROOT && minor == 0 {
			prev = q
		}

		if q.Type() == "mq" && q.Attrs().Parent == netlink.HANDLE_ROOT && q.Attrs().Handle == netlink.MakeHandle(1, 0) {
			return nil
		}
	}
	if prev != nil {
		_ = QdiscDel(prev)
	}

	return QdiscReplace(&netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_ROOT,
			Handle:    netlink.MakeHandle(1, 0),
		},
		QdiscType: "mq",
	})
}
