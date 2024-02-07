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

package hooks

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/sirupsen/logrus"
)

// ContextHook is a hook for trace reuqestID
type ContextHook struct{}

// Levels returns the list of logging levels on which the hook is triggered.
func (h *ContextHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire is the main method which is called every time when logger has an error
// or warning message.
func (h *ContextHook) Fire(entry *logrus.Entry) error {
	ctx := entry.Context
	if ctx == nil {
		return nil
	}

	if traceID := ctx.Value(logfields.TraceID); traceID != nil {
		entry.Data[logfields.TraceID] = traceID.(string)
	}

	return nil
}
