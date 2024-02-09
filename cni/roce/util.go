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
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"syscall"
	"time"

	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
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

func (p *rocePlugin) getRdmaDeviceFromMetaAPI(ctx context.Context) (map[string]string, error) {
	macList, macErr := p.metaClient.ListMacs()
	if macErr != nil {
		return nil, macErr
	}
	log.Infof(ctx, "macList:%v", macList)

	defaultRouteInterface, err := p.netutil.DetectDefaultRouteInterfaceName()
	if err != nil {
		log.Errorf(ctx, "get default route interface error:%v", err)
	}

	m := make(map[string]string)
	for _, macAddress := range macList {
		vifFeatures, vifErr := p.metaClient.GetVifFeatures(macAddress)
		if vifErr != nil {
			return nil, fmt.Errorf("get mac %s vif features failed: %v", macAddress, vifErr)
		}
		if DeviceRequireIpvlanMaps[strings.TrimSpace(vifFeatures)] {
			target, err := p.netutil.GetLinkByMacAddress(macAddress)
			if err != nil {
				return nil, err
			}

			if target.Attrs().Name == defaultRouteInterface {
				continue
			}
			m[target.Attrs().Name] = vifFeatures
		}
	}
	log.Infof(ctx, "rdma device List:%v", m)

	return m, nil
}

func (p *rocePlugin) getAllRdmaDevices(ctx context.Context) (map[string]string, error) {
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

		log.Infof(ctx, "read rdma device from file: %v", m)

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
			m, err := p.getRdmaDeviceFromMetaAPI(ctx)
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

			log.Infof(ctx, "update file content:%v", m)
		}
		return m, nil
	} else if os.IsNotExist(err) {
		m, err := p.getRdmaDeviceFromMetaAPI(ctx)
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

		log.Infof(ctx, "create file,content:%v", m)
		return m, nil
	} else {
		return nil, fmt.Errorf("check file error:%v", err)
	}
}
