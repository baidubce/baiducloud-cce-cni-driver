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

package metrics

import (
	"fmt"
	"reflect"

	"github.com/sirupsen/logrus"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/components"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
)

// LoggingHook is a hook for logrus which counts error and warning messages as a
// Prometheus metric.
type LoggingHook struct {
	metric CounterVec
}

// NewLoggingHook returns a new instance of LoggingHook for the given CCE
// component.
func NewLoggingHook(component string) *LoggingHook {
	// NOTE(mrostecki): For now errors and warning metric exists only for CCE
	// daemon, but support of Prometheus metrics in some other components (i.e.
	// cce-health - GH-4268) is planned.

	// Pick a metric for the component.
	var metric CounterVec
	switch component {
	case components.CCEAgentName:
		metric = ErrorsWarnings
	case components.CCEOperatortName:
		metric = ErrorsWarnings
	default:
		panic(fmt.Sprintf("component %s is unsupported by LoggingHook", component))
	}

	return &LoggingHook{metric: metric}
}

// Levels returns the list of logging levels on which the hook is triggered.
func (h *LoggingHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.ErrorLevel,
		logrus.WarnLevel,
	}
}

// Fire is the main method which is called every time when logger has an error
// or warning message.
func (h *LoggingHook) Fire(entry *logrus.Entry) error {
	// Get information about subsystem from logging entry field.
	iSubsystem, ok := entry.Data[logfields.LogSubsys]
	if !ok {
		serializedEntry, err := entry.String()
		if err != nil {
			return fmt.Errorf("log entry cannot be serialized and doesn't contain 'subsys' field")
		}
		return fmt.Errorf("log entry doesn't contain 'subsys' field: %s", serializedEntry)
	}
	subsystem, ok := iSubsystem.(string)
	if !ok {
		return fmt.Errorf("type of the 'subsystem' log entry field is not string but %s", reflect.TypeOf(iSubsystem))
	}

	// Increment the metric.
	h.metric.WithLabelValues(entry.Level.String(), subsystem).Inc()

	return nil
}
