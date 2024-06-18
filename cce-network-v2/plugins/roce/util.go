/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/containernetworking/cni/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	filename        = "/var/run/rdmadevice.info"
	eriVifFeatures  = "elastic_rdma"
	roceVifFeatures = "rdma_roce"
)

var DeviceRequireIpvlanMaps = map[string]bool{
	"elastic_rdma": true,  //eri
	"elastic_nic":  false, //eni
	"rdma_roce":    true,  //roce
	"rdma_ib":      false, //ib
}
var syncInterval = 5.0 //minute

func (p *rocePlugin) getRdmaDeviceFromMetaAPI(logger *logrus.Entry) (map[string]string, error) {
	macList, macErr := p.metaClient.ListMacs()
	if macErr != nil {
		return nil, macErr
	}
	logger.Infof("macList:%v", macList)

	defaultRouteInterface, err := DetectDefaultRouteInterfaceName()
	if err != nil {
		logger.Errorf("get default route interface error:%v", err)
	}

	m := make(map[string]string)
	for _, macAddress := range macList {
		vifFeatures, vifErr := p.metaClient.GetVifFeatures(macAddress)
		if vifErr != nil {
			return nil, fmt.Errorf("get mac %s vif features failed: %v", macAddress, vifErr)
		}
		if DeviceRequireIpvlanMaps[strings.TrimSpace(vifFeatures)] {
			target, err := GetLinkByMacAddress(macAddress)
			if err != nil {
				return nil, err
			}

			if target.Attrs().Name == defaultRouteInterface {
				continue
			}
			m[target.Attrs().Name] = vifFeatures
		}
	}
	logger.Infof("rdma device List:%v", m)

	return m, nil
}

func (p *rocePlugin) getAllRdmaDevices(logger *logrus.Entry) (map[string]string, error) {
	if _, err := os.Stat(filename); err == nil {
		data, err := ioutil.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read file failed:%v", err)
		}

		m := make(map[string]string)
		err = json.Unmarshal(data, &m)
		if err != nil {
			return nil, fmt.Errorf("nnmarshal error: %v", err)
		}

		logger.Infof("read rdma device from file: %v", m)

		info, err := os.Stat(filename)
		if err != nil {
			return nil, fmt.Errorf("get file info error:%v", err)
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, fmt.Errorf("type assert error!")
		}

		ctime := time.Unix(int64(stat.Ctim.Sec), int64(stat.Ctim.Nsec))
		now := time.Now()
		diff := now.Sub(ctime).Minutes()

		if diff > syncInterval {
			m, err := p.getRdmaDeviceFromMetaAPI(logger)
			if err != nil {
				return m, err
			}

			data, err := json.MarshalIndent(m, "", "  ")
			if err != nil {
				return m, fmt.Errorf("marshal failed:%v", err)
			}

			err = ioutil.WriteFile(filename, data, 0644)
			if err != nil {
				return m, fmt.Errorf("write file error:%v", err)
			}

			logger.Infof("update file content:%v", m)
		}
		return m, nil
	} else if os.IsNotExist(err) {
		m, err := p.getRdmaDeviceFromMetaAPI(logger)
		if err != nil {
			return nil, err
		}

		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal failed:%v", err)
		}

		err = ioutil.WriteFile(filename, data, 0644)
		if err != nil {
			return nil, fmt.Errorf("write file failed:%v", err)
		}

		logger.Infof("create file,content:%v", m)
		return m, nil
	} else {
		return nil, fmt.Errorf("check file error:%v", err)
	}
}

func IsNotExistError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT || errno == syscall.ESRCH
	} else {
		log.Printf("Error %v is not type of syscall.Errno", err)
	}

	return false
}

func DetectDefaultRouteInterfaceName() (string, error) {
	routeToDstIP, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return "", err
	}

	for _, v := range routeToDstIP {
		if v.Dst == nil {
			l, err := netlink.LinkByIndex(v.LinkIndex)
			if err != nil {
				return "", err
			}
			return l.Attrs().Name, nil
		}
	}

	return "", fmt.Errorf("no default route interface found")
}

func GetLinkByMacAddress(macAddress string) (netlink.Link, error) {
	interfaces, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	var link netlink.Link

	for _, intf := range interfaces {
		if intf.Attrs().HardwareAddr.String() == macAddress {
			link = intf
			break
		}
	}

	if link == nil {
		return nil, fmt.Errorf("link with MAC %s not found", macAddress)
	}

	return link, nil
}

type K8SArgs struct {
	types.CommonArgs `json:"commonArgs"`
	// IP is pod's ip address
	IP net.IP `json:"ip"`
	// K8S_POD_NAME is pod's name
	K8S_POD_NAME types.UnmarshallableString `json:"k8s_pod_name"`
	// K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_NAMESPACE types.UnmarshallableString `json:"k8s_pod_namespace"`
	// K8S_POD_INFRA_CONTAINER_ID is pod's container ID
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString `json:"k8s_pod_infra_container_id"`
}
