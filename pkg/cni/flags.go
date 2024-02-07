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

package cni

import (
	"flag"

	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	LogFileMaxSizeMB = "1000"
)

func InitFlags(logFile string) {
	log.InitFlags(nil)
	_ = flag.Set("logtostderr", "false")
	_ = flag.Set("log_file", logFile)
	_ = flag.Set("log_file_max_size", LogFileMaxSizeMB)
	flag.Parse()
}
