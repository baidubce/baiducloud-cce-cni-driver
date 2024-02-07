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

package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	runtime_client "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"

	clientapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
)

type Client struct {
	clientapi.CceAPI
}

// DefaultSockPath returns default UNIX domain socket path or
// path set using CILIUM_SOCK env variable
func DefaultSockPath() string {
	// Check if environment variable points to socket
	e := os.Getenv(defaults.SockPathEnv)
	if e == "" {
		// If unset, fall back to default value
		e = defaults.SockPath
	}
	return "unix://" + e

}

func configureTransport(tr *http.Transport, proto, addr string) *http.Transport {
	if tr == nil {
		tr = &http.Transport{}
	}

	if proto == "unix" {
		// No need for compression in local communications.
		tr.DisableCompression = true
		tr.DialContext = func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial(proto, addr)
		}
	} else {
		tr.Proxy = http.ProxyFromEnvironment
		tr.DialContext = (&net.Dialer{}).DialContext
	}

	return tr
}

// NewDefaultClient creates a client with default parameters connecting to UNIX domain socket.
func NewDefaultClient() (*Client, error) {
	return NewClient("")
}

// NewDefaultClientWithTimeout creates a client with default parameters connecting to UNIX
// domain socket and waits for cce-agent availability.
func NewDefaultClientWithTimeout(timeout time.Duration) (*Client, error) {
	timeoutAfter := time.After(timeout)
	var c *Client
	var err error
	for {
		select {
		case <-timeoutAfter:
			return nil, fmt.Errorf("failed to create cce agent client after %f seconds timeout: %s", timeout.Seconds(), err)
		default:
		}

		c, err = NewDefaultClient()
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for {
			select {
			case <-timeoutAfter:
				return nil, fmt.Errorf("failed to create cce agent client after %f seconds timeout: %s", timeout.Seconds(), err)
			default:
			}
			// This is an API call that we do to the cce-agent to check
			// if it is up and running.
			// _, err = c.Daemon.GetConfig(nil)
			// if err != nil {
			// 	time.Sleep(500 * time.Millisecond)
			// 	continue
			// }
			return c, nil
		}
	}
}

// NewClient creates a client for the given `host`.
// If host is nil then use SockPath provided by CILIUM_SOCK
// or the cce default SockPath
func NewClient(host string) (*Client, error) {
	if host == "" {
		host = DefaultSockPath()
	}
	tmp := strings.SplitN(host, "://", 2)
	if len(tmp) != 2 {
		return nil, fmt.Errorf("invalid host format '%s'", host)
	}

	switch tmp[0] {
	case "tcp":
		if _, err := url.Parse("tcp://" + tmp[1]); err != nil {
			return nil, err
		}
		host = "http://" + tmp[1]
	case "unix":
		host = tmp[1]
	}

	transport := configureTransport(nil, tmp[0], host)
	httpClient := &http.Client{Transport: transport}
	clientTrans := runtime_client.NewWithClient(tmp[1], clientapi.DefaultBasePath,
		clientapi.DefaultSchemes, httpClient)
	return &Client{*clientapi.New(clientTrans, strfmt.Default)}, nil
}

// Hint tries to improve the error message displayed to the user.
func Hint(err error) error {
	if err == nil {
		return err
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("CCE API client timeout exceeded")
	}

	e, _ := url.PathUnescape(err.Error())
	if strings.Contains(err.Error(), defaults.SockPath) {
		return fmt.Errorf("%s\nIs the agent running?", e)
	}
	return fmt.Errorf("%s", e)
}

func timeSince(since time.Time) string {
	out := "never"
	if !since.IsZero() {
		t := time.Since(since)
		out = t.Truncate(time.Second).String() + " ago"
	}

	return out
}

func stateUnhealthy(state string) bool {
	return state == models.StatusStateWarning ||
		state == models.StatusStateFailure
}

func statusUnhealthy(s *models.Status) bool {
	if s != nil {
		return stateUnhealthy(s.State)
	}
	return false
}

type StatusDetails struct {
	// AllAddress causes all addresses to be printed by FormatStatusResponse.
	AllAddresses bool
	// AllControllers causes all controllers to be printed by FormatStatusResponse.
	AllControllers bool
	// AllNodes causes all nodes to be printed by FormatStatusResponse.
	AllNodes bool
	// AllRedirects causes all redirects to be printed by FormatStatusResponse.
	AllRedirects bool
	// AllClusters causes all clusters to be printed by FormatStatusResponse.
	AllClusters bool
	// BPFMapDetails causes BPF map details to be printed by FormatStatusResponse.
	BPFMapDetails bool
	// KubeProxyReplacementDetails causes BPF kube-proxy details to be printed by FormatStatusResponse.
	KubeProxyReplacementDetails bool
	// ClockSourceDetails causes BPF time-keeping internals to be printed by FormatStatusResponse.
	ClockSourceDetails bool
}

var (
	// StatusAllDetails causes no additional status details to be printed by
	// FormatStatusResponse.
	StatusNoDetails = StatusDetails{}
	// StatusAllDetails causes all status details to be printed by FormatStatusResponse.
	StatusAllDetails = StatusDetails{
		AllAddresses:                true,
		AllControllers:              true,
		AllNodes:                    true,
		AllRedirects:                true,
		AllClusters:                 true,
		BPFMapDetails:               true,
		KubeProxyReplacementDetails: true,
		ClockSourceDetails:          true,
	}
)
