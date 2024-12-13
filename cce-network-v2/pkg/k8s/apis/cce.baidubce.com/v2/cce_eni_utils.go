/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */
package v2

import (
	"net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *ENIStatus) AppendVPCStatus(vpcStatus VPCENIStatus) {
	if s.VPCStatus != vpcStatus {
		s.VPCStatus = vpcStatus

		change := ENIStatusChange{
			StatusChange: StatusChange{
				Code: "ok",
				Time: metav1.Now(),
			},
			VPCStatus: vpcStatus,
		}

		if len(s.VPCStatusChangeLog) < 20 {
			s.VPCStatusChangeLog = append(s.VPCStatusChangeLog, change)
		} else {
			s.VPCStatusChangeLog = s.VPCStatusChangeLog[1:19]
			s.VPCStatusChangeLog = append(s.VPCStatusChangeLog, change)
		}
	}
}

func (s *ENIStatus) AppendCCEENIStatus(status CCEENIStatus) {
	if s.CCEStatus != status {
		s.CCEStatus = status
		change := ENIStatusChange{
			StatusChange: StatusChange{
				Code: "ok",
				Time: metav1.Now(),
			},
			CCEENIStatus: status,
		}

		if len(s.CCEStatusChangeLog) < 20 {
			s.CCEStatusChangeLog = append(s.CCEStatusChangeLog, change)
		} else {
			s.CCEStatusChangeLog = s.CCEStatusChangeLog[1:19]
			s.CCEStatusChangeLog = append(s.CCEStatusChangeLog, change)
		}
	}
}

func FiltePrimaryAddress(IPs []*models.PrivateIP) []*models.PrivateIP {
	var primaryIPs = make([]*models.PrivateIP, 0)

	for i := 0; i < len(IPs); i++ {
		if IPs[i].Primary {
			primaryIPs = append(primaryIPs, IPs[i])
		}
	}
	return primaryIPs
}

func (s CCEENIStatus) IsReadyOnNode() bool {
	return s == ENIStatusReadyOnNode || s == ENIStatusUsingInPod
}

// GetPrimaryIP get the primary IP of ENI from the IP list
func GetPrimaryIPs(ips []*models.PrivateIP) *models.PrivateIP {
	for i := 0; i < len(ips); i++ {
		if ips[i].Primary {
			return ips[i]
		}
	}
	return nil
}

func IPFamilyByIP(ipstr string) IPFamily {
	ip := net.ParseIP(ipstr)
	if ip == nil {
		return IPv4Family
	}
	if ip.To4() == nil {
		return IPv6Family
	}
	return IPv4Family
}
