package bandwidth

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/features"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint/event"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/sysctl"
)

const (
	bandwidthSys = "bandwidth-manager"

	AnnotaionPodIngressBandwidth = "kubernetes.io/ingress-bandwidth"
	AnnotaionPodEgressBandwidth  = "kubernetes.io/egress-bandwidth"
	AnnotaionPodBindwidthMode    = "kubernetes.io/bindwidth-mode"
)

var (
	GlobalManager *BandwidthManager
	managerLog    = logging.NewSubysLogger(bandwidthSys)
)

func GetPodBandwidth(podAnnotation map[string]string, cniDriver string) (*ccev2.BindwidthOption, error) {
	var opt = ccev2.BindwidthOption{
		Mode: GlobalManager.Mode,
	}
	if len(podAnnotation) == 0 {
		return &opt, nil
	}
	if mode, ok := podAnnotation[AnnotaionPodBindwidthMode]; ok {
		switch mode {
		case ccev2.BindwidthModeEDT:
			opt.Mode = ccev2.BindwidthModeEDT
		case ccev2.BindwidthModeTC:
			opt.Mode = ccev2.BindwidthModeTC
		}
	}
	if ingressBandwidth, ok := podAnnotation[AnnotaionPodIngressBandwidth]; ok {
		if ingress, err := parseBandwidth(ingressBandwidth); err == nil {
			opt.Ingress = ingress
		} else {
			return nil, fmt.Errorf("failed to parse annotaion ingress bandwidth %s", ingressBandwidth)
		}
	}
	if egressBandwidth, ok := podAnnotation[AnnotaionPodEgressBandwidth]; ok {
		if egress, err := parseBandwidth(egressBandwidth); err == nil {
			opt.Egress = egress
		} else {
			return nil, fmt.Errorf("failed to parse annotaion egress bandwidth %s", egressBandwidth)
		}
	}
	return &opt, nil
}

// bandwidth limit unit
const (
	BYTE = 1 << (10 * iota)
	KILOBYTE
	MEGABYTE
	GIGABYTE
	TERABYTE
)

func parseBandwidth(s string) (int64, error) {
	// when bandwidth is "", return
	if len(s) == 0 {
		return 0, fmt.Errorf("invalid bandwidth %s", s)
	}

	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	i := strings.IndexFunc(s, unicode.IsLetter)

	bytesString, multiple := s[:i], s[i:]
	bytes, err := strconv.ParseFloat(bytesString, 64)
	if err != nil || bytes <= 0 {
		return 0, fmt.Errorf("invalid bandwidth %s", s)
	}

	switch multiple {
	case "T", "TB", "TIB":
		return int64(bytes * TERABYTE), nil
	case "G", "GB", "GIB":
		return int64(bytes * GIGABYTE), nil
	case "M", "MB", "MIB":
		return int64(bytes * MEGABYTE), nil
	case "K", "KB", "KIB":
		return int64(bytes * KILOBYTE), nil
	case "B", "":
		return int64(bytes), nil
	default:
		return 0, fmt.Errorf("invalid bandwidth %s", s)
	}
}

type BandwidthManager struct {
	Mode ccev2.BindwidthMode
}

// AcceptType implements event.EndpointProbeEventHandler.
func (*BandwidthManager) AcceptType() event.EndpointProbeEventType {
	return event.EndpointProbeEventBandwidth
}

// Handle implements event.EndpointProbeEventHandler.
func (manager *BandwidthManager) Handle(event *event.EndpointProbeEvent) (*ccev2.ExtFeatureStatus, error) {
	if event.Obj.Spec.Network.Bindwidth == nil ||
		event.Obj.Spec.ExternalIdentifiers == nil {
		return nil, fmt.Errorf("invalid event %v", event)
	}

	var (
		opt             = event.Obj.Spec.Network.Bindwidth
		now             = metav1.Now()
		bandwidthStatus = &ccev2.ExtFeatureStatus{
			ContainerID: event.Obj.Spec.ExternalIdentifiers.ContainerID,
			Data: map[string]string{
				"mode":    string(opt.Mode),
				"ingress": strconv.FormatInt(opt.Ingress, 10),
				"egress":  strconv.FormatInt(opt.Egress, 10),
			},
			UpdateTime: &now,
		}
	)

	scopeLog := managerLog.WithFields(logrus.Fields{
		"endpoint": event.ID,
		"mode":     opt.Mode,
		"ingress":  opt.Ingress,
		"egress":   opt.Egress,
	})
	scopeLog.Info("bandwidth manager received event")

	cctx, err := link.NewContainerContext(event.Obj.Spec.ExternalIdentifiers.ContainerID, event.Obj.Spec.ExternalIdentifiers.Netns)
	if err != nil {
		bandwidthStatus.Msg = fmt.Sprintf("failed to get container context: %v", err)
		scopeLog.WithError(err).Error("failed to get container context")
		return bandwidthStatus, nil
	}
	defer cctx.Close()

	switch opt.Mode {
	case ccev2.BindwidthModeEDT:
		managerLog.Warn("bandwidth mode edt have not been implemented yet")
		return bandwidthStatus, nil
	default:
		switch cctx.Driver {
		case string(models.DatapathModeVeth), "":
			err = manager.setVethTC(cctx, opt)
			if err != nil {
				bandwidthStatus.Msg = fmt.Sprintf("failed to set veth tc: %v", err)
				return bandwidthStatus, err
			}
		}

	}
	bandwidthStatus.Ready = true
	return bandwidthStatus, nil
}

func InitBandwidthManager() {
	if !option.Config.EnableBandwidthManager {
		return
	}
	GlobalManager = &BandwidthManager{}
	ProbeBandwidthManager()
}

func ProbeBandwidthManager() {

	// We at least need 5.1 kernel for native TCP EDT integration
	// and writable queue_mapping that we use. Below helper is
	// available for 5.1 kernels and onwards.
	kernelGood := features.HaveProgramHelper(ebpf.SchedCLS, asm.FnSkbEcnSetCe) == nil

	if _, err := sysctl.Read("net.core.default_qdisc"); err != nil {
		managerLog.WithError(err).Warn("BPF bandwidth manager could not read procfs. Disabling the feature.")
		GlobalManager.Mode = ccev2.BindwidthModeTC
		return
	}
	if !kernelGood {
		managerLog.Warn("BPF bandwidth manager needs kernel 5.1 or newer. Disabling the feature.")
		GlobalManager.Mode = ccev2.BindwidthModeTC
		return
	}

	// TODO: check if we can use eBPF to implement EDT
	// manager.Mode = ccev2.BindwidthModeEDT

	// we can use TC to implement bandwidth
	GlobalManager.Mode = ccev2.BindwidthModeTC
}

var _ event.EndpointProbeEventHandler = &BandwidthManager{}
