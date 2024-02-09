package eni

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/services/eni"
)

const (
	modeEri = "highPerformance"
	modeEni = "standard"
)

type Client struct {
	*eni.Client
}

func NewClient(ak, sk, endPoint string) (*Client, error) {
	eniClient, err := eni.NewClient(ak, sk, endPoint)
	if err != nil {
		return nil, err
	}
	return &Client{
		Client: eniClient,
	}, nil
}

// ListEnis - list all eni without eri
//
//	PARAMS:
//	- args: the arguments to list all eni without eri
//	RETURNS:
//	- *ListEniResult: the result of list all eni
//	- error: nil if success otherwise the specific error
func (c *Client) ListEnis(args *eni.ListEniArgs) (*eni.ListEniResult, error) {
	return c.listEniOrEris(args, isEni)
}

// ListEris - list all eri with the specific parameters
//
//	PARAMS:
//	- args: the arguments to list all eri
//	RETURNS:
//	- *ListEniResult: the result of list all eri
//	- error: nil if success otherwise the specific error
func (c *Client) ListEris(args *eni.ListEniArgs) (*eni.ListEniResult, error) {
	return c.listEniOrEris(args, isEri)
}

func (c *Client) listEniOrEris(args *eni.ListEniArgs, filterFunc func(eriOrEni Eni) bool) (*eni.ListEniResult, error) {
	eniAndEriList, err := c.listEniAndEris(args)
	if err != nil {
		return nil, err
	}

	eniOrEriList := make([]eni.Eni, 0)
	for i := range eniAndEriList.Eni {
		eniOrEri := eniAndEriList.Eni[i]
		if filterFunc(eniOrEri) {
			eniOrEriList = append(eniOrEriList, eniOrEri.Eni)
		}
	}

	erisResult := &eni.ListEniResult{
		Eni:         eniOrEriList,
		Marker:      eniAndEriList.Marker,
		IsTruncated: eniAndEriList.IsTruncated,
		NextMarker:  eniAndEriList.NextMarker,
		MaxKeys:     eniAndEriList.MaxKeys,
	}

	return erisResult, err
}

func (c *Client) listEniAndEris(args *eni.ListEniArgs) (*ListEniResult, error) {
	if args == nil {
		return nil, fmt.Errorf("the ListEniArgs cannot be nil")
	}
	if args.MaxKeys == 0 {
		args.MaxKeys = 1000
	}

	result := &ListEniResult{}
	builder := bce.NewRequestBuilder(c).
		WithURL(getURLForEni()).
		WithMethod(http.GET).
		WithQueryParam("vpcId", args.VpcId).
		WithQueryParamFilter("instanceId", args.InstanceId).
		WithQueryParamFilter("name", args.Name).
		WithQueryParamFilter("marker", args.Marker).
		WithQueryParamFilter("maxKeys", strconv.Itoa(args.MaxKeys))

	if len(args.PrivateIpAddress) != 0 {
		builder.WithQueryParam("privateIpAddress",
			strings.Replace(strings.Trim(fmt.Sprint(args.PrivateIpAddress), "[]"), " ", ",", -1))
	}

	err := builder.WithResult(result).Do()

	return result, err
}

func getURLForEni() string {
	return eni.URI_PREFIX + eni.REQUEST_ENI_URL
}

func isEri(eriOrEni Eni) bool {
	if strings.EqualFold(eriOrEni.NetworkInterfaceTrafficMode, modeEri) {
		return true
	}
	return false
}

func isEni(eriOrEni Eni) bool {
	if strings.EqualFold(eriOrEni.NetworkInterfaceTrafficMode, modeEni) {
		return true
	}
	return false
}
