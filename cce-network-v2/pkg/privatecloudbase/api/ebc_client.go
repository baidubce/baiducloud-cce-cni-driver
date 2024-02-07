package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/transport"
)

var (
	log = logging.DefaultLogger.WithField(logfields.LogSubsys, "pcb-api")
)

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type Client interface {
	IPAllocateClient
	SubnetClient
}

// IPAllocateClient Client to private cloud base
// https://ku.baidu-int.com/knowledge/HFVrC7hq1Q/bPDEaFBbnd/04hxQCVwYK/ntxk1F6-P7lVRi#anchor-4b444560-5383-11ed-9d27-81f612d08f43
// group.POST("/acquire_ip_by_subnet", h.AcquireIPBySubnet)
// group.POST("/acquire_ip_by_purpose", h.AcquireIPByPurpose)
// group.POST("/release_ip", h.ReleaseIP)
// group.POST("/batch_acquire_ip_by_subnet", h.BatchAcquireIPBySubnet)
// group.POST("/batch_acquire_ip_by_purpose", h.BatchAcquireIPByPurpose)
// group.POST("/batch_release_ip", h.BatchReleaseIP)
// group.GET("/allocated_ips/:Region/:id", h.GetAllocatedIP)
// group.GET("/allocated_ips/:Region", h.ListAllocatedIP)
// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type IPAllocateClient interface {
	// AcquireIPBySubnet 指定网段申请IP
	//Request
	//* Method: POST
	//* URL: /ipam/v1/acquire_ip_by_subnet
	//* Headers： Content-Type:application/json
	//* Body: AcquireIPBySubnetRequest
	AcquireIPBySubnet(ctx context.Context, req AcquireIPBySubnetRequest) (*AllocatedIPResponse, error)
	// BatchAcquireIPBySubnet 指定网段批量申请IP
	//目前未实现将ID视为token做幂等处理，在请求失败时，可能会造成IP已分配但无人使用的问题。
	//Request
	//* Method: POST
	//* URL: /ipam/v1/batch_acquire_ip_by_subnet
	//* Headers： Content-Type:application/json
	//* Body: BatchAcquireIPBySubnetRequest
	BatchAcquireIPBySubnet(ctx context.Context, req BatchAcquireIPBySubnetRequest) (*ListAllocatedIPResponse, error)

	// BatchReleaseIP 批量释放IP
	//Request
	//* Method: POST
	//* URL: /ipam/v1/batch_release_ip
	//* Headers： Content-Type:application/json
	//* Body: BatchReleaseIPRequest
	BatchReleaseIP(ctx context.Context, req BatchReleaseIPRequest) error

	// AllocatedIPs 查询已分配IP
	//Request
	//* Method: PUT
	//* URL: /ipam/v1/allocated_ips/:Region
	//* Headers： Content-Type:application/json
	//* Params:
	//  * ListAllocatedIPRequest （详见数据结构）
	AllocatedIPs(ctx context.Context) (*ListAllocatedIPResponse, error)
}

// SubnetClient to private cloud base
// https://ku.baidu-int.com/knowledge/HFVrC7hq1Q/bPDEaFBbnd/04hxQCVwYK/ntxk1F6-P7lVRi#anchor-4b444560-5383-11ed-9d27-81f612d08f43
// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type SubnetClient interface {
	// CreateSubnet 创建网段
	//Request
	//* Method: POST
	//* URL: /ipam/v1/subnets
	//* Headers： Content-Type:application/json
	// GetIPPool GET /v1/ippool/{{poolname}}
	CreateSubnet(ctx context.Context, subnet Subnet) (*SubnetResponse, error)

	// group.GET("/subnets/:Region", h.ListSubnet)
	ListSubnets(ctx context.Context) (*ListSubnetResponse, error)
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type PrivateCloudBaseClient struct {
	client *http.Client
	host   string
	Region string
}

func (p *PrivateCloudBaseClient) AcquireIPBySubnet(ctx context.Context, req AcquireIPBySubnetRequest) (*AllocatedIPResponse, error) {
	resp := &AllocatedIPResponse{}
	err := p.post(ctx, "acquire_ip_by_subnet", &req, resp)
	if err == nil {
		if resp.AllocatedIP.Subnet != "" && req.Subnet != "" && resp.AllocatedIP.Subnet != req.Subnet {
			return resp, fmt.Errorf("response sbunet %s is not same as the request %s", resp.AllocatedIP.Subnet, req.Subnet)
		}
	}
	return resp, err
}

func (p *PrivateCloudBaseClient) BatchAcquireIPBySubnet(ctx context.Context, req BatchAcquireIPBySubnetRequest) (*ListAllocatedIPResponse, error) {
	resp := &ListAllocatedIPResponse{}
	err := p.post(ctx, "batch_acquire_ip_by_subnet", &req, resp)
	return resp, err
}

func (p *PrivateCloudBaseClient) BatchReleaseIP(ctx context.Context, req BatchReleaseIPRequest) error {
	resp := &ErrorResponse{}
	return p.post(ctx, "batch_release_ip", &req, resp)
}

func (p *PrivateCloudBaseClient) AllocatedIPs(ctx context.Context) (*ListAllocatedIPResponse, error) {
	resp := &ListAllocatedIPResponse{}
	err := p.get(ctx, fmt.Sprintf("allocated_ips/%s", p.Region), nil, resp)
	return resp, err
}

func (p *PrivateCloudBaseClient) CreateSubnet(ctx context.Context, subnet Subnet) (*SubnetResponse, error) {
	resp := &SubnetResponse{}
	err := p.post(ctx, fmt.Sprintf("subnets/%s", p.Region), &subnet, resp)
	return resp, err
}

func (p *PrivateCloudBaseClient) ListSubnets(ctx context.Context) (*ListSubnetResponse, error) {
	req := &ListSubnetRequest{}
	resp := &ListSubnetResponse{}
	err := p.get(ctx, fmt.Sprintf("subnets/%s", p.Region), req, resp)
	return resp, err
}

func NewClient(host, region, userAgent string) (Client, error) {
	dail := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	trans, err := transport.New(&transport.Config{
		UserAgent: userAgent,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dail.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          1,
			MaxConnsPerHost:       10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	})
	if err != nil {
		return nil, err
	}

	c := &PrivateCloudBaseClient{
		client: &http.Client{
			Transport: trans,
			Timeout:   time.Second * 30,
		},
		host:   host,
		Region: region,
	}
	return c, err

}

func (p *PrivateCloudBaseClient) post(ctx context.Context, path string, reqData interface{}, respData ResponseInterface) error {
	if setR, ok := reqData.(SetRegion); ok {
		setR.SetRegion(p.Region)
	}
	jsonData, _ := json.Marshal(reqData)
	url := p.completeURL(path)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("create POST %s req error %v", path, err)
	}
	request.Header.Set("Content-Type", "application/json")

	return doRequest(err, p, request, url, jsonData, respData)
}

func (p *PrivateCloudBaseClient) get(ctx context.Context, path string, reqData interface{}, respData ResponseInterface) error {
	url := p.completeURL(path)

	jsonData := "{}"
	if setR, ok := reqData.(SetRegion); ok {
		setR.SetRegion(p.Region)
		b, _ := json.Marshal(reqData)
		jsonData = string(b)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, bytes.NewBufferString(jsonData))
	if err != nil {
		return fmt.Errorf("create GET %s req error %v", path, err)
	}
	request.Header.Set("Content-Type", "application/json")

	return doRequest(err, p, request, url, nil, respData)
}

func doRequest(err error, p *PrivateCloudBaseClient, request *http.Request, url string, requestBody []byte, respData ResponseInterface) error {
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("post %s error %v", url, err)
	}

	responseBody, err := ioutil.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		log.WithFields(logrus.Fields{
			"module":   "PrivateCloudBaseClient",
			"url":      url,
			"request":  string(requestBody),
			"response": string(responseBody),
			"error":    err,
			"code":     response.StatusCode,
		}).Errorf("http request error")
		return fmt.Errorf("request failed %d", response.StatusCode)
	}
	log.WithFields(logrus.Fields{
		"module":   "PrivateCloudBaseClient",
		"url":      url,
		"request":  string(requestBody),
		"response": string(responseBody),
		"method":   request.Method,
		"code":     response.StatusCode,
	}).Debug("request success")

	if respData != nil {
		respData.SetCode(response.StatusCode)
		err = json.Unmarshal(responseBody, respData)
		if err != nil {
			return fmt.Errorf("unmarshal body error %w", err)
		}
		if !respData.IsSuccess() {
			log.WithFields(logrus.Fields{
				"module":     "PrivateCloudBaseClient",
				"url":        url,
				"request":    string(requestBody),
				"response":   string(responseBody),
				"error":      err,
				"statusCode": response.StatusCode,
			}).Errorf("http request error")
			return fmt.Errorf("request failed")
		}
	}
	return nil
}

// completeURL Complete the host and token for the request address
func (p *PrivateCloudBaseClient) completeURL(path string) string {
	return fmt.Sprintf("%s/ipam/v1/%s?clientToken=%s", p.host, path, uuid.New().String())
}
