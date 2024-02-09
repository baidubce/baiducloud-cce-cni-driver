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

// Package pprof enables use of pprof in CCE
package pprof

import (
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"strconv"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

var log = logging.DefaultLogger.WithField(logfields.LogSubsys, "pprof")

// Enable runs an HTTP server to serve the pprof API
func Enable(port int) {
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
	var apiAddress = net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	go func() {
		if err := http.ListenAndServe(apiAddress, nil); err != nil {
			log.WithError(err).Warn("Unable to serve pprof API")
		}
	}()
}
