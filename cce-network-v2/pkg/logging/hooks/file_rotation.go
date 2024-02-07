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
	"github.com/cilium/lumberjack/v2"
	"github.com/sirupsen/logrus"
)

// FileRotationOption provides all parameters for file rotation
type FileRotationOption struct {
	FileName   string
	MaxSize    int
	MaxAge     int
	MaxBackups int
	LocalTime  bool
	Compress   bool
}

type Option func(*FileRotationOption)

// WithMaxSize provides way to adjust maxSize (in MBs). Defaults to
// 100 MBs.
func WithMaxSize(maxSize int) Option {
	return func(option *FileRotationOption) {
		option.MaxSize = maxSize
	}
}

// WithMaxAge provides way to adjust max age (in days). The default is
// not to remove old log files based on age.
func WithMaxAge(maxAge int) Option {
	return func(option *FileRotationOption) {
		option.MaxAge = maxAge
	}
}

// WithMaxBackups provides way to adjust max number of backups. Defaults
// to retain all old log files though MaxAge may still cause them to get
// deleted.
func WithMaxBackups(MaxBackups int) Option {
	return func(option *FileRotationOption) {
		option.MaxBackups = MaxBackups
	}
}

// EnableLocalTime is to determine if the time used for formatting the
// timestamps in backup files is the computer's local time.  The default
// is to use UTC time.
func EnableLocalTime() Option {
	return func(option *FileRotationOption) {
		option.LocalTime = true
	}
}

// EnableCompression is to enable old log file gzip compression. Defaults
// to false.
func EnableCompression() Option {
	return func(option *FileRotationOption) {
		option.Compress = true
	}
}

// FileRotationLogHook stores the configuration of the hook
type FileRotationLogHook struct {
	logger *lumberjack.Logger
}

// NewFileRotationLogHook creates a new FileRotationLogHook*/
func NewFileRotationLogHook(fileName string, opts ...Option) *FileRotationLogHook {
	options := &FileRotationOption{
		FileName:  fileName,
		MaxSize:   100,   // MBs
		LocalTime: false, // UTC
		Compress:  false, // no compression with gzip
	}

	for _, opt := range opts {
		opt(options)
	}

	logger := &lumberjack.Logger{
		Filename:   options.FileName,
		MaxSize:    options.MaxSize,
		MaxAge:     options.MaxAge,
		MaxBackups: options.MaxBackups,
		LocalTime:  options.LocalTime,
		Compress:   options.Compress,
	}

	return &FileRotationLogHook{
		logger: logger,
	}
}

// Fire is called when a log event is fired.
func (hook *FileRotationLogHook) Fire(entry *logrus.Entry) error {
	line, err := entry.String()
	if err != nil {
		return err
	}

	_, err = hook.logger.Write([]byte(line))
	if err != nil {
		return err
	}

	return nil
}

// Levels returns the available logging levels
func (hook *FileRotationLogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}
