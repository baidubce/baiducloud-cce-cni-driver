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

package fs

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
)

type FileSystem interface {
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	Open(name string) (*os.File, error)
	Rename(oldpath string, newpath string) error
	Remove(name string) error
	MD5Sum(name string) (string, error)
}

var _ FileSystem = &DefaultFS{}

// DefaultFS implements FileSystem using the local disk
type DefaultFS struct{}

func (DefaultFS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}
func (DefaultFS) ReadFile(name string) ([]byte, error) {
	return ioutil.ReadFile(name)
}

func (DefaultFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	return ioutil.WriteFile(name, data, perm)
}

func (DefaultFS) Open(name string) (*os.File, error) {
	return os.Open(name)
}

func (DefaultFS) Rename(oldpath string, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (DefaultFS) Remove(name string) error {
	return os.Remove(name)
}

func (DefaultFS) MD5Sum(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
