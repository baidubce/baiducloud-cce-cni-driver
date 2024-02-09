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

package logger

import (
	"context"
	"flag"
	"fmt"

	"k8s.io/klog"
)

// ContextKeyType context key
type ContextKeyType string

const (
	// TraceID context key name
	TraceID ContextKeyType = "TraceId"
)

func InitFlags(flagset *flag.FlagSet) {
	klog.InitFlags(flagset)
}

func Flush() {
	klog.Flush()
}

func Info(ctx context.Context, args ...interface{}) {
	prefix := buildFormat(ctx, "")
	klog.InfoDepth(1, prefix+fmt.Sprint(args...))
}

func Infof(ctx context.Context, format string, args ...interface{}) {
	format = buildFormat(ctx, format)
	klog.InfoDepth(1, fmt.Sprintf(format, args...))
}

func Warning(ctx context.Context, args ...interface{}) {
	prefix := buildFormat(ctx, "")
	klog.WarningDepth(1, prefix+fmt.Sprint(args...))
}

func Warningf(ctx context.Context, format string, args ...interface{}) {
	format = buildFormat(ctx, format)
	klog.WarningDepth(1, fmt.Sprintf(format, args...))
}

func Error(ctx context.Context, args ...interface{}) {
	prefix := buildFormat(ctx, "")
	klog.ErrorDepth(1, prefix+fmt.Sprint(args...))
}

func Errorf(ctx context.Context, format string, args ...interface{}) {
	format = buildFormat(ctx, format)
	klog.ErrorDepth(1, fmt.Sprintf(format, args...))
}

func Fatal(ctx context.Context, args ...interface{}) {
	prefix := buildFormat(ctx, "")
	klog.FatalDepth(1, prefix+fmt.Sprint(args...))
}

func Fatalf(ctx context.Context, format string, args ...interface{}) {
	format = buildFormat(ctx, format)
	klog.FatalDepth(1, fmt.Sprintf(format, args...))
}

func buildFormat(ctx context.Context, format string) string {
	if ctx != nil {
		if traceID := ctx.Value(TraceID); traceID != nil {
			format = "[" + (string)(TraceID) + ": " + ctx.Value(TraceID).(string) + "] " + format
		}
	}
	return format
}

func NewContext() context.Context {
	traceID := GetUUID()
	ctx := context.WithValue(context.TODO(), TraceID, traceID)
	return ctx
}

func EnsureTraceIDInCtx(ctx context.Context) context.Context {
	if ctx.Value(TraceID) == nil {
		ctx = context.WithValue(ctx, TraceID, GetUUID())
	}
	return ctx
}

type Verbose klog.Verbose

func V(level klog.Level) Verbose {
	return Verbose(klog.V(level))
}

func (v Verbose) Info(ctx context.Context, args ...interface{}) {
	if v {
		prefix := buildFormat(ctx, "")
		klog.InfoDepth(1, prefix+fmt.Sprint(args...))
	}
}

func (v Verbose) Infof(ctx context.Context, format string, args ...interface{}) {
	if v {
		format = buildFormat(ctx, format)
		klog.InfoDepth(1, fmt.Sprintf(format, args...))
	}
}
