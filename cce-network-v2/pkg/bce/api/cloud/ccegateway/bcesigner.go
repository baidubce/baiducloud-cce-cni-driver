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

package ccegateway

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
	corev1 "k8s.io/api/core/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

const (
	TokenKey     = "token"
	ExpiredAtKey = "expiredAt"

	TokenSecretName      = "cce-plugin-token"
	TokenSecretNamespace = "kube-system"

	TokenHeaderKey      = "cce-token"
	ClusterIDHeaderKey  = "cce-cluster"
	RemoteHostHeaderKey = "cce-remote-host"

	EndpointOverrideEnv = "CCE_GATEWAY_ENDPOINT"
)

var (
	log = logging.NewSubysLogger("cce-gateway")
)

var _ BCESigner = (*bceSigner)(nil)

type BCESigner interface {
	auth.Signer

	SetVolumeSource(tokenVolume string)
	SetSecretSource(secretGetter func(namespace, name string) (*corev1.Secret, error))
	SetTokenSource(tokenGetter func(*auth.BceCredentials, *auth.SignOptions) ([]byte, []byte, error))
	GetHostAndPort() (string, int)
}

type bceSigner struct {
	Region    string
	ClusterID string
	Token     string
	ExpiredAt int64

	tokenVolume  string
	secretGetter func(namespace, name string) (*corev1.Secret, error)
	tokenGetter  func(cred *auth.BceCredentials, opt *auth.SignOptions) ([]byte, []byte, error)
}

func NewBCESigner(region, clusterID string) *bceSigner {
	return &bceSigner{
		Region:    region,
		ClusterID: clusterID,
	}
}

func (cg *bceSigner) SetVolumeSource(tokenVolume string) {
	cg.tokenVolume = tokenVolume
}

func (cg *bceSigner) SetSecretSource(secretGetter func(namespace, name string) (*corev1.Secret, error)) {
	cg.secretGetter = secretGetter
}

func (cg *bceSigner) SetTokenSource(tokenGetter func(*auth.BceCredentials, *auth.SignOptions) ([]byte, []byte, error)) {
	cg.tokenGetter = tokenGetter
}

func (cg bceSigner) GetHostAndPort() (string, int) {
	host, port := "cce-gateway.bj.baidubce.com", 80 // default host and port

	if cg.Region != "" {
		host = "cce-gateway." + cg.Region + ".baidubce.com"
	}

	if env := os.Getenv(EndpointOverrideEnv); env != "" {
		// use the endpoint explicitly set in env if exists
		hostPort := strings.Split(env, ":")
		host = hostPort[0]
		if len(hostPort) == 2 {
			if portNum, err := strconv.Atoi(hostPort[1]); err == nil {
				port = portNum
			}
		}
	}
	return host, port
}

func (cg *bceSigner) Sign(req *http.Request, cred *auth.BceCredentials, opt *auth.SignOptions) {
	if err := cg.ensureToken(cred, opt); err != nil {
		log.WithError(err).Error("ensureToken failed")
		return
	}

	req.SetHeader(TokenHeaderKey, cg.Token)
	req.SetHeader(ClusterIDHeaderKey, cg.ClusterID)
	// add default content-type if not set
	if req.Header("Content-Type") == "" {
		req.SetHeader("Content-Type", "application/json")
	}

	// add x-bce-request-id if not set
	if req.Header(http.BCE_REQUEST_ID) == "" {
		req.SetHeader(http.BCE_REQUEST_ID, util.NewRequestId())
	}

	gatewayHost, gatewayPort := cg.GetHostAndPort()
	reqHost := fmt.Sprintf("%s:%d", gatewayHost, gatewayPort)
	// make Sign idempotent as it may be invoked many times on retry
	if req.Host() != reqHost {
		req.SetHeader(RemoteHostHeaderKey, req.Host())
		req.SetHeader(http.HOST, reqHost)
		req.SetHost(reqHost)
	}
}

func (cg *bceSigner) ensureToken(cred *auth.BceCredentials, opt *auth.SignOptions) error {
	if cg == nil {
		return fmt.Errorf("bceSigner for cce-gateway is nil")
	}
	if cg.Token != "" && time.Now().Unix() < cg.ExpiredAt {
		// Our token is still valid, just use it.
		return nil
	}

	var tokenBytes, expiredAtBytes []byte
	var succeeded bool
	var lastErr error

	// Iterate all valid sources until token is successfully fetched.

	if !succeeded && cg.secretGetter != nil {
		err := func() error {
			tokenSecret, err := cg.secretGetter(TokenSecretNamespace, TokenSecretName)
			if err != nil {
				return fmt.Errorf("fail to get secret %s/%s: %w",
					TokenSecretNamespace, TokenSecretName, err)
			}
			if tokenSecret == nil {
				return fmt.Errorf("token secret is nil")
			}
			var ok bool
			tokenBytes, ok = tokenSecret.Data[TokenKey]
			if !ok {
				return fmt.Errorf("fail to find token key=%s in secret", TokenKey)
			}
			expiredAtBytes, ok = tokenSecret.Data[ExpiredAtKey]
			if !ok {
				return fmt.Errorf("fail to find expiredAt key=%s in secret", ExpiredAtKey)
			}
			return nil
		}()
		if err != nil {
			log.WithError(err).Error("fetch token from secret failed")
			lastErr = err
		} else {
			succeeded = true
		}
	}

	if !succeeded && cg.tokenVolume != "" {
		err := func() error {
			var err error
			tokenFile := filepath.Join(cg.tokenVolume, TokenKey)
			tokenBytes, err = ioutil.ReadFile(tokenFile)
			if err != nil {
				return fmt.Errorf("fail to read %s: %w", tokenFile, err)
			}
			expiredAtFile := filepath.Join(cg.tokenVolume, ExpiredAtKey)
			expiredAtBytes, err = ioutil.ReadFile(expiredAtFile)
			if err != nil {
				return fmt.Errorf("fail to read %s: %w", expiredAtFile, err)
			}
			return nil
		}()
		if err != nil {
			log.WithError(err).Error("fetch token from volume failed")
			lastErr = err
		} else {
			succeeded = true
		}
	}

	if !succeeded && cg.tokenGetter != nil {
		var err error
		tokenBytes, expiredAtBytes, err = cg.tokenGetter(cred, opt)
		if err != nil {
			log.WithError(err).Error("fetch token from getter failed")
			lastErr = fmt.Errorf("fail to invoke tokenGetter: %w", err)
		} else {
			succeeded = true
		}
	}

	if !succeeded {
		return fmt.Errorf("ensureToken failed with lastErr=%v", lastErr)
	}

	cg.ExpiredAt, lastErr = strconv.ParseInt(string(expiredAtBytes), 10, 64)
	if lastErr != nil {
		return fmt.Errorf("fail to parse expiredAt=%s: %w", string(expiredAtBytes), lastErr)
	}
	cg.Token = string(tokenBytes)

	return nil
}
