package roce

import (
	"context"
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	corev1 "k8s.io/api/core/v1"
)

func (ipam *IPAM) findMatchENIs(ctx context.Context, pod *corev1.Pod) (string, error) {
	log.Infof(ctx, "start to find instanceID for node (%v/%v) in roce ipam", pod.Namespace, pod.Name)
	// get node
	node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
	if err != nil {
		log.Errorf(ctx, "get node (%v/%v) in roce ipam from kubeInformer has err %v", pod.Namespace, pod.Name, err)
		return "", err
	}

	instanceID, err := util.GetInstanceIDFromNode(node)
	if err != nil {
		msg := fmt.Sprintf("get instanceID for roce ipam from %v node is failed: %v", node, err)
		log.Error(ctx, msg)
		return "", err
	}
	log.Infof(ctx, "get instanceID is %s in roce ipam from %s node", instanceID, node.Name)
	return instanceID, nil
}

func (ipam *IPAM) findMatchedEniByMac(ctx context.Context, instanceID string, macAddress string) (string, *hpc.EniList, error) {
	log.Infof(ctx, "start to find suitable eni by mac for instanceID %v/%v", instanceID, macAddress)
	hpcEni, err := ipam.cloud.GetHPCEniID(ctx, instanceID)
	if err != nil {
		log.Errorf(ctx, "failed to get hpc eniID: %v", err)
		return "", hpcEni, err
	}
	log.Infof(ctx, "hpcEni.HpcEniResult count: %v, result is %s", hpcEni.Result, len(hpcEni.Result))

	for _, roceInfo := range hpcEni.Result {
		if roceInfo.MacAddress == macAddress {
			log.Infof(ctx, "find hpcEni %s macAddress is match macAddress %s, eniID is %s", roceInfo.MacAddress, macAddress, roceInfo.EniID)
			return roceInfo.EniID, hpcEni, nil
		}
	}
	return "", hpcEni, nil
}

func (ipam *IPAM) addRocePrivateIP(ctx context.Context, eniID string, instanceID string, hpcEniList *hpc.EniList) (string, error) {
	log.Infof(ctx, "start to add hpc privateIP from eniID: %v", eniID)
	args := &hpc.EniBatchPrivateIPArgs{
		EniID:                 eniID,
		PrivateIPAddressCount: 1, // 当前指定一个
	}
	hpcEni, err := ipam.cloud.BatchAddHpcEniPrivateIP(ctx, args)
	if err != nil {
		log.Errorf(ctx, "failed to batch add privateIp in  %s hpcEni: %v", eniID, err)
		return "", err
	}

	log.Infof(ctx, "batch add HpcEni privateIp is %v", hpcEni.PrivateIPAddresses)

	if len(hpcEni.PrivateIPAddresses) != 0 && hpcEni.PrivateIPAddresses != nil {
		if hpcEni.PrivateIPAddresses[0] != "" {
			log.Infof(ctx, "roce privateIP from eniID is %s", hpcEni.PrivateIPAddresses[0])
			return hpcEni.PrivateIPAddresses[0], nil
		}
	} else {
		//如果没有去eniID list接口查看一下
		log.Infof(ctx, "start to find suitable eni by mac for instanceID %v/%v", instanceID)
		hpcEni, err := ipam.cloud.GetHPCEniID(ctx, instanceID)
		if err != nil {
			log.Errorf(ctx, "failed to get hpc eniID: %v", err)
			return "", err
		}
		privateIP := ipam.getDiff(hpcEniList, hpcEni, eniID)
		log.Infof(ctx, "get diff privateIp is %s from eniID is %s", privateIP, eniID)
		return privateIP, nil
	}
	return "", nil
}

func (ipam *IPAM) getDiff(old, new *hpc.EniList, eniID string) string {
	var newPrivateIp, oldPrivateIp []hpc.PrivateIP
	for _, eni := range new.Result {
		if eni.EniID == eniID {
			log.Infof(context.TODO(), "get enid %s from new hpcEni result", eniID)
			newPrivateIp = eni.PrivateIPSet
			break
		}
	}

	for _, eni := range old.Result {
		if eni.EniID == eniID {
			log.Infof(context.TODO(), "get enid %s from old hpcEni result", eniID)
			oldPrivateIp = eni.PrivateIPSet
			break
		}
	}
	privateIP := comparedPrivateIp(newPrivateIp, oldPrivateIp)
	return privateIP
}

func comparedPrivateIp(new []hpc.PrivateIP, old []hpc.PrivateIP) string {
	ipmap := make(map[string]bool)
	for _, pip := range old {
		if _, exist := ipmap[pip.PrivateIPAddress]; !exist {
			ipmap[pip.PrivateIPAddress] = pip.Primary
		}
	}
	log.Infof(context.TODO(), "make old ip map, count is %d", len(ipmap))
	for _, newip := range new {
		if isPrivateIp, exist := ipmap[newip.PrivateIPAddress]; !exist && !isPrivateIp {
			log.Infof(context.TODO(), "new %s ip is not exist in ond result, primary is %v ", newip.PrivateIPAddress, isPrivateIp)
			return newip.PrivateIPAddress
		}
	}
	return ""
}

func (ipam *IPAM) deleteRocePrivateIP(ctx context.Context, eniID string, ip string) error {
	log.Infof(ctx, "start to delete roce privateIP from %s eniID and %s ip.", eniID, ip)
	args := &hpc.EniBatchDeleteIPArgs{
		EniID:              eniID,
		PrivateIPAddresses: []string{ip},
	}
	log.Infof(ctx, "delete roce privateIP is %v", args)

	err := ipam.cloud.BatchDeleteHpcEniPrivateIP(ctx, args)
	if err != nil {
		log.Errorf(ctx, "failed to delete roce private ip: %v", err)
		return err
	}
	return nil
}
