package hpc

type Interface interface {
}
type BatchAddPrivateIPResult struct {
	PrivateIPAddresses []string `json:"privateIpAddresses"`
}

type EniBatchPrivateIPArgs struct {
	EniID                 string `json:"eniId"`
	PrivateIPAddressCount int    `json:"privateIpAddressCount,omitempty"`
}

type EniBatchDeleteIPArgs struct {
	EniID              string   `json:"eniId"`
	PrivateIPAddresses []string `json:"privateIpAddresses,omitempty"`
}

type Result struct {
	EniID        string      `json:"eniId"`
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	InstanceID   string      `json:"instanceId"`
	MacAddress   string      `json:"macAddress"`
	Status       string      `json:"status"`
	PrivateIPSet []PrivateIP `json:"privateIpSet"`
	CreatedTime  string      `json:"createdTime"`
}

type EniList struct {
	Result      []Result `json:"result"`
	Marker      string   `json:"marker"`
	IsTruncated bool     `json:"isTruncated"`
	NextMarker  string   `json:"nextMarker"`
	MaxKeys     int      `json:"maxKeys"`
}

type PrivateIP struct {
	Primary          bool   `json:"primary"`
	PrivateIPAddress string `json:"privateIpAddress"`
}
