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
	r.HandleFunc(metadataBasePath+"instance-type", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"instance-type-ex", handler).Methods("GET")
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
	var instanceType InstanceType
	var instanceTypeEx InstanceTypeEx
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

	instanceType, err = c.GetInstanceType()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	if instanceType != "xxx" {
		t.Errorf("TestMetaData want: %v , got : %v", "xxx", instanceType)
	}

	instanceTypeEx, err = c.GetInstanceTypeEx()
	if err != nil {
		t.Errorf("TestMetaData got error: %v", err)
	}

	if instanceTypeEx != InstanceTypeExUnknown {
		t.Errorf("TestMetaData want: %v , got : %v", "xxx", instanceTypeEx)
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

	_, err = c.GetInstanceType()
	if err != ErrorNotImplemented {
		t.Errorf("TestMetaData got error: %v", err)
	}

	_, err = c.GetInstanceTypeEx()
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

	_, err = c.GetInstanceType()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}

	_, err = c.GetInstanceTypeEx()
	if err == nil {
		t.Errorf("TestMetaData wants error")
	}
}

func handleMetaDataGetInstanceTypeBCC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("bcc.ic4.c16m16"))
}

func handleMetaDataGetInstanceTypeBBC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("BBC-I4-01S"))
}

func handleMetaDataGetInstanceTypeEBC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ebc.l5.c128m512.1d"))
}

func handleMetaDataGetInstanceTypeExBCC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("bcc"))
}

func handleMetaDataGetInstanceTypeExBBC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("bbc"))
}

func newInstanceTypeHandler(handlerInstanceType func(w http.ResponseWriter, r *http.Request),
	handlerInstanceTypeEx func(w http.ResponseWriter, r *http.Request)) http.Handler {
	r := mux.NewRouter()

	r.HandleFunc(metadataBasePath+"instance-type", handlerInstanceType).Methods("GET")
	r.HandleFunc(metadataBasePath+"instance-type-ex", handlerInstanceTypeEx).Methods("GET")

	return r
}

func TestMetaDataGetInstanceTypeEx(t *testing.T) {
	type fields struct {
		handler http.Handler
	}
	tests := []struct {
		name    string
		fields  fields
		want    InstanceTypeEx
		wantErr bool
	}{
		{
			name: "normal bcc case",
			fields: func() fields {
				hl := newInstanceTypeHandler(handleMetaDataGetInstanceTypeBCC, handleMetaDataGetInstanceTypeExBCC)
				return fields{
					handler: hl,
				}
			}(),
			want:    InstanceTypeExBCC,
			wantErr: false,
		},
		{
			name: "normal bbc case",
			fields: func() fields {
				hl := newInstanceTypeHandler(handleMetaDataGetInstanceTypeBBC, handleMetaDataGetInstanceTypeExBBC)
				return fields{
					handler: hl,
				}
			}(),
			want:    InstanceTypeExBBC,
			wantErr: false,
		},
		{
			name: "unknow case",
			fields: func() fields {
				hl := newHandler(handleMetaDataOK)
				return fields{
					handler: hl,
				}
			}(),
			want:    InstanceTypeExUnknown,
			wantErr: false,
		},
		{
			name: "normal ebc case",
			fields: func() fields {
				hl := newInstanceTypeHandler(handleMetaDataGetInstanceTypeEBC, handleMetaDataGetInstanceTypeExBBC)
				return fields{
					handler: hl,
				}
			}(),

			want:    InstanceTypeExBCC,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		defer func() {
			tearDownTestEnv()
		}()
		t.Run(tt.name, func(t *testing.T) {
			setupTestEnv(tt.fields.handler)

			url, _ := url.Parse(testHTTPServer.URL)
			c := NewClient()
			c.host = url.Host
			c.scheme = url.Scheme

			got, err := c.GetInstanceTypeEx()
			if (err != nil) != tt.wantErr {
				t.Errorf("MetaData.GetInstanceTypeEx() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("MetaData.GetInstanceTypeEx() = %v, want %v", got, tt.want)
			}
		})
	}
}
