package eni

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
)

var (
	eriClient *Client
)

type TestServerConfig struct {
	t                  *testing.T
	ass                *assert.Assertions
	RequestMethod      string
	RequestURLPath     string
	RequestBody        []byte
	RequestHeaders     map[string]string
	RequestQueryParams map[string]string
	ResponseHeaders    map[string]string
	ResponseBody       []byte
	ResponseBodyFunc   func(t *testing.T, actualBody []byte)
	ResponseStatusCode int
	HookAfterResponse  func()
	Debug              bool
}

func NewMockClient(endpoint string) *Client {
	eriClient, _ = NewClient("dfsdfsfs", "dfsfdsfs", endpoint)
	return eriClient
}

func NewTestServer(t *testing.T, config *TestServerConfig) *httptest.Server {
	config.ass = assert.New(t)
	return httptest.NewServer(config)
}

func (config *TestServerConfig) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ass := config.ass
	defer r.Body.Close()

	if config.RequestMethod != "" {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Method: ")
			spew.Fdump(os.Stderr, r.Method)
		}
		ass.Equal(config.RequestMethod, r.Method, "request method mismatch")
	}

	if config.RequestURLPath != "" {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Path: ")
			spew.Fdump(os.Stderr, r.URL.Path)
		}
		ass.Equal(config.RequestURLPath, r.URL.Path, "request path mismatch")
	}

	if config.Debug && len(config.RequestHeaders) > 0 {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Header: ")
			spew.Fdump(os.Stderr, r.Header)
		}
	}
	reqHeaders := r.Header
	for k, want := range config.RequestHeaders {
		actual := reqHeaders.Get(k)
		ass.Equal(want, actual, fmt.Sprintf("header '%s' mismatch", k))
	}

	if len(config.RequestQueryParams) > 0 {
		if config.Debug {
			fmt.Fprintf(os.Stderr, "QueryParams: ")
			spew.Fdump(os.Stderr, r.URL.RawQuery)
		}
	}
	reqQueryParams := r.URL.Query()
	for k, want := range config.RequestQueryParams {
		actual := reqQueryParams.Get(k)
		ass.Equal(want, actual, fmt.Sprintf("query param '%s' mismatch", k))
	}

	if len(config.RequestBody) > 0 {
		body, _ := ioutil.ReadAll(r.Body)
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Body: ")
			spew.Fdump(os.Stderr, body)
		}
		if config.ResponseBodyFunc != nil {
			config.ResponseBodyFunc(config.t, body)
		} else {
			ass.Equal(config.RequestBody, body, "request body mismatch")
		}
	}

	// 返回顺序要遵守：header -> status code -> body
	rspHeaders := w.Header()
	for k, v := range config.ResponseHeaders {
		rspHeaders.Set(k, v)
	}

	if config.ResponseStatusCode > 0 {
		w.WriteHeader(config.ResponseStatusCode)
	}

	if len(config.ResponseBody) > 0 {
		write, _ := w.Write(config.ResponseBody)
		if config.Debug {
			fmt.Fprintf(os.Stderr, "Body: ")
			spew.Fdump(os.Stderr, write)
		}
	}

	if config.HookAfterResponse != nil {
		config.HookAfterResponse()
	}
}

func Test_ListEris(t *testing.T) {
	ass := assert.New(t)
	tests := []struct {
		name    string
		context context.Context
		err     error
		config  *TestServerConfig
		args    *eni.ListEniArgs

		expect int
	}{
		{
			name: "normal",
			config: &TestServerConfig{
				RequestMethod:   http.MethodGet,
				RequestURLPath:  "/v1/eni",
				ResponseHeaders: map[string]string{"Content-Type": "application/json"},
				ResponseBody: []byte("{\"enis\":[{\"networkInterfaceTrafficMode\":\"highPerformance\"}," +
					"{\"networkInterfaceTrafficMode\":\"standard\"}," +
					"{\"networkInterfaceTrafficMode\":\"highPerformance\"}]}"),
			},
			args: &eni.ListEniArgs{
				VpcId:            "fsdf",
				PrivateIpAddress: []string{"10.0.0.1"},
			},
			expect: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svr := NewTestServer(t, test.config)
			client := NewMockClient(svr.URL)
			result, actualErr := client.ListEris(test.args)
			if test.err == nil {
				ass.Nil(actualErr, actualErr)
				ass.Equal(test.expect, len(result.Eni))
			} else {
				ass.ErrorContains(actualErr, test.err.Error(), "err mismatch")
			}
			svr.Close()
		})
	}
}

func Test_ListEnis(t *testing.T) {
	ass := assert.New(t)
	tests := []struct {
		name    string
		context context.Context
		err     error
		config  *TestServerConfig
		args    *eni.ListEniArgs

		expect int
	}{
		{
			name: "normal",
			config: &TestServerConfig{
				RequestMethod:   http.MethodGet,
				RequestURLPath:  "/v1/eni",
				ResponseHeaders: map[string]string{"Content-Type": "application/json"},
				ResponseBody: []byte("{\"enis\":[{\"networkInterfaceTrafficMode\":\"highPerformance\"}," +
					"{\"networkInterfaceTrafficMode\":\"standard\"}," +
					"{\"networkInterfaceTrafficMode\":\"highPerformance\"}]}"),
			},
			args: &eni.ListEniArgs{
				VpcId:            "fsdf",
				PrivateIpAddress: []string{"10.0.0.1"},
			},
			expect: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svr := NewTestServer(t, test.config)
			client := NewMockClient(svr.URL)
			result, actualErr := client.ListEnis(test.args)
			if test.err == nil {
				ass.Nil(actualErr, actualErr)
				ass.Equal(test.expect, len(result.Eni))
			} else {
				ass.ErrorContains(actualErr, test.err.Error(), "err mismatch")
			}
			svr.Close()
		})
	}
}
