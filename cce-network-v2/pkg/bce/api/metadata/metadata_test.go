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
	"strings"
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
	r.HandleFunc(metadataBasePath+"source", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/vif_features", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/mtu", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.100/gateway", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.100/mask", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.100/ip_type", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.101/ip_type", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"network/interfaces/macs", handler).Methods("GET")
	r.HandleFunc(metadataBasePath+"feature_list", handler).Methods("GET")

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

func handleFeatureListOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"eni_quota":"15","eri_quota":"1"}`))
}

func handleFeatureListInvalidJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"eni_quota":"15","eri_quota":"1"`))
}

func handleMTUOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("9000"))
}

func handleMTUInvalidFormat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("invalid-mtu"))
}

func handleFixedIPsOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("10.0.1.100/\n10.0.1.101/\n"))
}

func handleIPTypePrimary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if strings.Contains(r.URL.Path, "10.0.1.100") {
		w.Write([]byte("primary"))
	} else {
		w.Write([]byte("secondary"))
	}
}

func handleGatewayOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("10.0.1.1"))
}

func handleMaskOK(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("255.255.255.0"))
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

func TestGetFeatureList(t *testing.T) {
	// Test successful response
	setupTestEnv(newHandler(handleFeatureListOK))
	defer tearDownTestEnv()

	url, _ := url.Parse(testHTTPServer.URL)
	c := NewClient()
	c.host = url.Host
	c.scheme = url.Scheme

	featureList, err := c.GetFeatureList()
	if err != nil {
		t.Errorf("GetFeatureList got error: %v", err)
	}

	if featureList.ENIQuota != "15" {
		t.Errorf("GetFeatureList ENIQuota want: %v, got: %v", "15", featureList.ENIQuota)
	}

	if featureList.ERIQuota != "1" {
		t.Errorf("GetFeatureList ERIQuota want: %v, got: %v", "1", featureList.ERIQuota)
	}
}

func TestGetFeatureListInvalidJSON(t *testing.T) {
	// Test invalid JSON response
	setupTestEnv(newHandler(handleFeatureListInvalidJSON))
	defer tearDownTestEnv()

	url, _ := url.Parse(testHTTPServer.URL)
	c := NewClient()
	c.host = url.Host
	c.scheme = url.Scheme

	_, err := c.GetFeatureList()
	if err == nil {
		t.Error("GetFeatureList should return error for invalid JSON")
	}
}

func TestGetFeatureListNotImplemented(t *testing.T) {
	// Test not implemented response
	setupTestEnv(newHandler(handleMetaDataNotImplemented))
	defer tearDownTestEnv()

	url, _ := url.Parse(testHTTPServer.URL)
	c := NewClient()
	c.host = url.Host
	c.scheme = url.Scheme

	_, err := c.GetFeatureList()
	if err != ErrorNotImplemented {
		t.Errorf("GetFeatureList should return ErrorNotImplemented, got: %v", err)
	}
}

func TestGetFeatureListError(t *testing.T) {
	// Test error response
	setupTestEnv(newHandler(handleMetaDataError))
	defer tearDownTestEnv()

	url, _ := url.Parse(testHTTPServer.URL)
	c := NewClient()
	c.host = url.Host
	c.scheme = url.Scheme

	_, err := c.GetFeatureList()
	if err == nil {
		t.Error("GetFeatureList should return error for server error")
	}
}

// Test GetLinkMTU function
func TestGetLinkMTU(t *testing.T) {
	// Test successful MTU response
	t.Run("TestGetLinkMTU_Given_ValidMTUResponse_When_CallGetLinkMTU_Then_ReturnCorrectMTU", func(t *testing.T) {
		setupTestEnv(newHandler(handleMTUOK))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		mtu, err := c.GetLinkMTU("test-mac")
		if err != nil {
			t.Errorf("GetLinkMTU got error: %v", err)
		}

		expectedMTU := 9000
		if mtu != expectedMTU {
			t.Errorf("GetLinkMTU want: %d, got: %d", expectedMTU, mtu)
		}
	})

	// Test invalid MTU format response
	t.Run("TestGetLinkMTU_Given_InvalidMTUFormat_When_CallGetLinkMTU_Then_ReturnError", func(t *testing.T) {
		setupTestEnv(newHandler(handleMTUInvalidFormat))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkMTU("test-mac")
		if err == nil {
			t.Error("GetLinkMTU should return error for invalid MTU format")
		}

		expectedErrorMsg := "failed to parse MTU value"
		if !strings.Contains(err.Error(), expectedErrorMsg) {
			t.Errorf("GetLinkMTU error should contain '%s', got: %v", expectedErrorMsg, err)
		}
	})

	// Test not implemented response
	t.Run("TestGetLinkMTU_Given_NotImplementedResponse_When_CallGetLinkMTU_Then_ReturnErrorNotImplemented", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataNotImplemented))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkMTU("test-mac")
		if err != ErrorNotImplemented {
			t.Errorf("GetLinkMTU should return ErrorNotImplemented, got: %v", err)
		}
	})

	// Test server error response
	t.Run("TestGetLinkMTU_Given_ServerError_When_CallGetLinkMTU_Then_ReturnError", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataError))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkMTU("test-mac")
		if err == nil {
			t.Error("GetLinkMTU should return error for server error")
		}
	})
}

// Test GetSourceType function
func TestGetSourceType(t *testing.T) {
	t.Run("TestGetSourceType_Given_ValidResponse_When_CallGetSourceType_Then_ReturnCorrectSource", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataOK))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		source, err := c.GetSourceType()
		if err != nil {
			t.Errorf("GetSourceType got error: %v", err)
		}

		if source != "xxx" {
			t.Errorf("GetSourceType want: %s, got: %s", "xxx", source)
		}
	})

	t.Run("TestGetSourceType_Given_NotImplemented_When_CallGetSourceType_Then_ReturnErrorNotImplemented", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataNotImplemented))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetSourceType()
		if err != ErrorNotImplemented {
			t.Errorf("GetSourceType should return ErrorNotImplemented, got: %v", err)
		}
	})
}

// Test GetLinkGateway function
func TestGetLinkGateway(t *testing.T) {
	t.Run("TestGetLinkGateway_Given_ValidResponse_When_CallGetLinkGateway_Then_ReturnCorrectGateway", func(t *testing.T) {
		setupTestEnv(newHandler(handleGatewayOK))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		gateway, err := c.GetLinkGateway("test-mac", "10.0.1.100")
		if err != nil {
			t.Errorf("GetLinkGateway got error: %v", err)
		}

		expectedGateway := "10.0.1.1"
		if gateway != expectedGateway {
			t.Errorf("GetLinkGateway want: %s, got: %s", expectedGateway, gateway)
		}
	})

	t.Run("TestGetLinkGateway_Given_ServerError_When_CallGetLinkGateway_Then_ReturnError", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataError))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkGateway("test-mac", "10.0.1.100")
		if err == nil {
			t.Error("GetLinkGateway should return error for server error")
		}
	})
}

// Test GetLinkMask function
func TestGetLinkMask(t *testing.T) {
	t.Run("TestGetLinkMask_Given_ValidResponse_When_CallGetLinkMask_Then_ReturnCorrectMask", func(t *testing.T) {
		setupTestEnv(newHandler(handleMaskOK))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		mask, err := c.GetLinkMask("test-mac", "10.0.1.100")
		if err != nil {
			t.Errorf("GetLinkMask got error: %v", err)
		}

		expectedMask := "255.255.255.0"
		if mask != expectedMask {
			t.Errorf("GetLinkMask want: %s, got: %s", expectedMask, mask)
		}
	})

	t.Run("TestGetLinkMask_Given_ServerError_When_CallGetLinkMask_Then_ReturnError", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataError))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkMask("test-mac", "10.0.1.100")
		if err == nil {
			t.Error("GetLinkMask should return error for server error")
		}
	})
}

// Test GetLinkFixedIPs function
func TestGetLinkFixedIPs(t *testing.T) {
	t.Run("TestGetLinkFixedIPs_Given_ValidResponse_When_CallGetLinkFixedIPs_Then_ReturnCorrectIPs", func(t *testing.T) {
		setupTestEnv(newHandler(handleFixedIPsOK))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		ips, err := c.GetLinkFixedIPs("test-mac")
		if err != nil {
			t.Errorf("GetLinkFixedIPs got error: %v", err)
		}

		expectedIPs := []string{"10.0.1.100", "10.0.1.101"}
		if len(ips) != len(expectedIPs) {
			t.Errorf("GetLinkFixedIPs want %d IPs, got %d", len(expectedIPs), len(ips))
		}

		for i, expectedIP := range expectedIPs {
			if i < len(ips) && ips[i] != expectedIP {
				t.Errorf("GetLinkFixedIPs want IP[%d]: %s, got: %s", i, expectedIP, ips[i])
			}
		}
	})

	t.Run("TestGetLinkFixedIPs_Given_ServerError_When_CallGetLinkFixedIPs_Then_ReturnError", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataError))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkFixedIPs("test-mac")
		if err == nil {
			t.Error("GetLinkFixedIPs should return error for server error")
		}
	})
}

// Test GetLinkPrimaryIP function
func TestGetLinkPrimaryIP(t *testing.T) {
	t.Run("TestGetLinkPrimaryIP_Given_ValidResponse_When_CallGetLinkPrimaryIP_Then_ReturnPrimaryIP", func(t *testing.T) {
		// Create a custom handler that handles both fixed_ips and ip_type endpoints
		r := mux.NewRouter()
		r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips", handleFixedIPsOK).Methods("GET")
		r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.100/ip_type", handleIPTypePrimary).Methods("GET")
		r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.101/ip_type", handleIPTypePrimary).Methods("GET")

		setupTestEnv(r)
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		primaryIP, err := c.GetLinkPrimaryIP("test-mac")
		if err != nil {
			t.Errorf("GetLinkPrimaryIP got error: %v", err)
		}

		expectedPrimaryIP := "10.0.1.100"
		if primaryIP != expectedPrimaryIP {
			t.Errorf("GetLinkPrimaryIP want: %s, got: %s", expectedPrimaryIP, primaryIP)
		}
	})

	t.Run("TestGetLinkPrimaryIP_Given_NoFixedIPs_When_CallGetLinkPrimaryIP_Then_ReturnError", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataError))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkPrimaryIP("test-mac")
		if err == nil {
			t.Error("GetLinkPrimaryIP should return error when no fixed IPs available")
		}
	})
}

// Test GetLinkSecondaryIPs function
func TestGetLinkSecondaryIPs(t *testing.T) {
	t.Run("TestGetLinkSecondaryIPs_Given_ValidResponse_When_CallGetLinkSecondaryIPs_Then_ReturnSecondaryIPs", func(t *testing.T) {
		// Create a custom handler that handles both fixed_ips and ip_type endpoints
		r := mux.NewRouter()
		r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips", handleFixedIPsOK).Methods("GET")
		r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.100/ip_type", handleIPTypePrimary).Methods("GET")
		r.HandleFunc(metadataBasePath+"network/interfaces/macs/test-mac/fixed_ips/10.0.1.101/ip_type", handleIPTypePrimary).Methods("GET")

		setupTestEnv(r)
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		secondaryIPs, err := c.GetLinkSecondaryIPs("test-mac")
		if err != nil {
			t.Errorf("GetLinkSecondaryIPs got error: %v", err)
		}

		// Based on our handler, 10.0.1.100 is primary, so 10.0.1.101 should be secondary
		expectedSecondaryIPs := []string{"10.0.1.101"}
		if len(secondaryIPs) != len(expectedSecondaryIPs) {
			t.Errorf("GetLinkSecondaryIPs want %d secondary IPs, got %d", len(expectedSecondaryIPs), len(secondaryIPs))
		}

		for i, expectedIP := range expectedSecondaryIPs {
			if i < len(secondaryIPs) && secondaryIPs[i] != expectedIP {
				t.Errorf("GetLinkSecondaryIPs want secondary IP[%d]: %s, got: %s", i, expectedIP, secondaryIPs[i])
			}
		}
	})

	t.Run("TestGetLinkSecondaryIPs_Given_NoPrimaryIP_When_CallGetLinkSecondaryIPs_Then_ReturnError", func(t *testing.T) {
		setupTestEnv(newHandler(handleMetaDataError))
		defer tearDownTestEnv()

		url, _ := url.Parse(testHTTPServer.URL)
		c := NewClient()
		c.host = url.Host
		c.scheme = url.Scheme

		_, err := c.GetLinkSecondaryIPs("test-mac")
		if err == nil {
			t.Error("GetLinkSecondaryIPs should return error when primary IP cannot be determined")
		}
	})
}
