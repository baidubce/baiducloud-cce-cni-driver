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

package metadata

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
)

var (
	testHTTPServer *httptest.Server
)

func setupTestEnv(handler http.Handler) {
	testHTTPServer = httptest.NewServer(handler)
}

func tearDownTestEnv() {
	testHTTPServer.Close()
}

func newHandler(handler func(w http.ResponseWriter, r *http.Request)) http.Handler {
	r := mux.NewRouter()

	r.HandleFunc(metadataBasePath+"instance-shortid", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"instance-name", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"local-ipv4", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"azone", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"region", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"vpc-id", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"subnet-id", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/vif_features", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs", handler).Methods("GET")

	return r
}

func handleMetaDataOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("xxx"))
}

func handleMetaDataNotImplemented(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(""))
}

func handleMetaDataError(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("internal server error"))
}

func TestMetaDataOK(t *testing.T) {
	setupTestEnv(newHandler(handleMetaDataOK))
	defer tearDownTestEnv()

	url, _ := url.Parse(testHTTPServer.URL)
	c := NewClient()
	c.host = url.Host
	c.scheme = url.Scheme

	var result string
	var err error

	result, err = c.GetInstanceID()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	result, err = c.GetInstanceName()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	result, err = c.GetLocalIPv4()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	result, err = c.GetAvailabilityZone()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	result, err = c.GetRegion()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	result, err = c.GetVPCID()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	result, err = c.GetSubnetID()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	if result != "xxx" {
		t.Errorf("TestMetaData want: %v , got : %v", "xxx", result)
	}

	result, err = c.GetVifFeatures("test-mac")
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}
	if result != "xxx" {
		t.Errorf("TestMetaData want: %v , got : %v", "xxx", result)
	}

	results, listErr := c.ListMacs()
	if listErr != nil {
		t.Errorf("TestMetaData got error: %v", listErr)
	}
	if len(results) != 1 {
		t.Errorf("TestMetaData len(results) wants: %d , got : %d", 1, len(results))
	}
}

func TestMetaDataNotImplemented(t *testing.T) {
	setupTestEnv(newHandler(handleMetaDataNotImplemented))
	defer tearDownTestEnv()

	url, _ := url.Parse(testHTTPServer.URL)
	c := NewClient()
	c.host = url.Host
	c.scheme = url.Scheme

	var err error

	_, err = c.GetInstanceID()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}

	_, err = c.GetInstanceName()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}

	_, err = c.GetLocalIPv4()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}

	_, err = c.GetAvailabilityZone()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}

	_, err = c.GetRegion()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}

	_, err = c.GetVPCID()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}

	_, err = c.GetSubnetID()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}
}

func TestMetaDataError(t *testing.T) {
	setupTestEnv(newHandler(handleMetaDataError))
	defer tearDownTestEnv()

	url, _ := url.Parse(testHTTPServer.URL)
	c := NewClient()
	c.host = url.Host
	c.scheme = url.Scheme

	var err error

	_, err = c.GetInstanceID()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}

	_, err = c.GetInstanceName()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}
	_, err = c.GetLocalIPv4()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}

	_, err = c.GetAvailabilityZone()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}

	_, err = c.GetRegion()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}

	_, err = c.GetVPCID()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}

	_, err = c.GetSubnetID()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}
}
