// Copyright 2021 Baidu Inc. All Rights Reserved.
// ~/baidu/inf/rollout-manager/testdata/testdata.go - location test data of unit test

// modification history
// --------------------
// 2021/04/07, by wangeweiwei22, create testdata

// DESCRIPTION

package testdata

import (
	"path/filepath"
	"runtime"
)

// basepath is the root directory of this package.
var basepath string

func init() {
	_, currentFile, _, _ := runtime.Caller(0)
	basepath = filepath.Dir(currentFile)
}

// Path returns the absolute path the given relative file or directory path,
// relative to the google.golang.org/grpc/testdata directory in the user's GOPATH.
// If rel is already absolute, it is returned unmodified.
func Path(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}

	return filepath.Join(basepath, rel)
}