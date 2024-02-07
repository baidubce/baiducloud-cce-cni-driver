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

package cloud

import (
	"context"
	"fmt"

	"github.com/baidubce/bce-sdk-go/auth"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/ccegateway"
)

const (
	AuthModeAccessKey  AuthMode = "key"
	AuthModeCCEGateway AuthMode = "gateway"
)

const (
	cceGatewayAKPlaceHolder = "cce-gateway-ak"
	cceGatewaySKPlaceHolder = "cce-gateway-sk"

	cceGatewayTokenVolume = "/var/run/secrets/cce/cce-plugin-token"
)

type AuthMode string

type Auth interface {
	GetSigner(ctx context.Context) auth.Signer
	GetCredentials(ctx context.Context) *auth.BceCredentials
	GetSignOptions(ctx context.Context) *auth.SignOptions
}

type accessKeyPair struct {
	ak    string
	sk    string
	token string
}

func NewAccessKeyPairAuth(ak, sk, token string) (Auth, error) {
	if ak == "" {
		return nil, fmt.Errorf("empty ak")
	}
	if sk == "" {
		return nil, fmt.Errorf("empty sk")
	}
	return &accessKeyPair{
		ak:    ak,
		sk:    sk,
		token: token,
	}, nil
}

func (keyPair *accessKeyPair) GetSigner(ctx context.Context) auth.Signer {
	return &auth.BceV1Signer{}
}

func (keyPair *accessKeyPair) GetCredentials(ctx context.Context) *auth.BceCredentials {
	return &auth.BceCredentials{
		AccessKeyId:     keyPair.ak,
		SecretAccessKey: keyPair.sk,
		SessionToken:    keyPair.token,
	}
}

func (keyPair *accessKeyPair) GetSignOptions(ctx context.Context) *auth.SignOptions {
	return &auth.SignOptions{
		HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
		ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS,
	}
}

type cceGateway struct {
	signer ccegateway.BCESigner
}

func NewCCEGatewayAuth(region, clusterID string, kubeClient kubernetes.Interface) (Auth, error) {
	if clusterID == "" {
		return nil, fmt.Errorf("empty cluster id")
	}

	signer := ccegateway.NewBCESigner(region, clusterID)

	// set two sources for signer
	// if fetching token from secret fails, fallback to volume
	signer.SetVolumeSource(cceGatewayTokenVolume)
	if kubeClient != nil {
		signer.SetSecretSource(func(namespace string, name string) (*v1.Secret, error) {
			return kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		})
	}

	return &cceGateway{
		signer: signer,
	}, nil
}

func (gateway *cceGateway) GetSigner(ctx context.Context) auth.Signer {
	return gateway.signer
}

func (gateway *cceGateway) GetCredentials(ctx context.Context) *auth.BceCredentials {
	return &auth.BceCredentials{
		AccessKeyId:     cceGatewayAKPlaceHolder,
		SecretAccessKey: cceGatewaySKPlaceHolder,
	}
}

func (gateway *cceGateway) GetSignOptions(ctx context.Context) *auth.SignOptions {
	return &auth.SignOptions{
		HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
		ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS,
	}
}
