// +k8s:deepcopy-gen=package,register
// +deepequal-gen=package
package api

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type AllocatedIP struct {
	ID        string `json:"id,omitempty"`
	IP        string `json:"ip,omitempty"`
	Mask      string `json:"mask,omitempty"`
	CIDR      string `json:"cidr,omitempty"`
	GW        string `json:"gw,omitempty"`
	NatGW     string `json:"natGW,omitempty"`
	Subnet    string `json:"subnet,omitempty"`
	Region    string `json:"Region,omitempty"`
	VPC       string `json:"vpc,omitempty"`
	ISP       string `json:"isp,omitempty"`
	IPVersion int    `json:"ipVersion,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
	VlanID    int    `json:"vlanID,omitempty"`
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type ResponseInterface interface {
	IsSuccess() bool
	SetCode(code int)
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type ErrorResponse struct {
	StatusCode               int
	Code, Message, RequestID string
}

func (receiver *ErrorResponse) IsSuccess() bool {
	//return receiver.StatusCode == 200 && receiver.Code == "200"
	return receiver.StatusCode == 200
}

func (receiver *ErrorResponse) SetCode(code int) {
	receiver.StatusCode = code
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type SetRegion interface {
	SetRegion(region string)
}

// 申请IP
// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type AcquireIPByPurposeRequest struct {
	ID               string `json:"id,omitempty" binding:"required"`
	Region           string `json:"Region,omitempty" binding:"required"`
	Purpose          string `json:"purpose,omitempty" binding:"required"`
	VPC              string `json:"vpc,omitempty"`
	Gateway          string `json:"gateway,omitempty"`
	ISP              string `json:"isp,omitempty"`
	IPVersion        int    `json:"ipVersion,omitempty"`
	SpecificIP       string `json:"specificIP,omitempty" `
	SpecificIPSuffix string `json:"specificIPSuffix,omitempty" `
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type AcquireIPBySubnetRequest struct {
	ID               string `json:"id,omitempty" binding:"required"`
	Region           string `json:"Region,omitempty" binding:"required"`
	SpecificIP       string `json:"specificIP,omitempty" `
	SpecificIPSuffix string `json:"specificIPSuffix,omitempty"`
	Subnet           string `json:"subnet,omitempty" binding:"required"`
}

func (req *AcquireIPBySubnetRequest) SetRegion(region string) {
	req.Region = region
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type AllocatedIPResponse struct {
	ErrorResponse `json:",inline"`
	AllocatedIP   AllocatedIP `json:"allocatedIP,omitempty"`
}

// 查询已申请IP
// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type ListAllocatedIPRequest struct {
	Region    string `json:"Region,omitempty"`
	Subnet    string `json:"subnet,omitempty"`
	VPC       string `json:"vpc,omitempty"`
	ISP       string `json:"isp,omitempty"`
	IPVersion int    `json:"ipVersion,omitempty"`
	CIDR      string `json:"cidr,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
}

func (req *ListAllocatedIPRequest) SetRegion(region string) {
	req.Region = region
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type ListAllocatedIPResponse struct {
	ErrorResponse `json:",inline"`
	AllocatedIPs  []AllocatedIP `json:"allocatedIPs,omitempty"`
}

// 批量申请IP
// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type BatchAcquireIPBySubnetRequest struct {
	ID     string `json:"id,omitempty" binding:"required"`
	Region string `json:"Region,omitempty" binding:"required"`
	Count  int    `json:"count,omitempty" binding:"required"`
	Subnet string `json:"subnet,omitempty" binding:"required"`
}

func (req *BatchAcquireIPBySubnetRequest) SetRegion(region string) {
	req.Region = region
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type BatchAcquireIPByPurposeRequest struct {
	ID        string `json:"id,omitempty" binding:"required"`
	Region    string `json:"Region,omitempty" binding:"required"`
	Count     int    `json:"count,omitempty" binding:"required"`
	Purpose   string `json:"purpose,omitempty" binding:"required"`
	VPC       string `json:"vpc,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	ISP       string `json:"isp,omitempty"`
	IPVersion int    `json:"ipVersion,omitempty"`
}

// 批量释放IP
// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type BatchReleaseIPRequest struct {
	VPC    string   `json:"vpc,omitempty" binding:"required"`
	Region string   `json:"Region,omitempty" binding:"required"`
	IPs    []string `json:"ips,omitempty"`
}

func (req *BatchReleaseIPRequest) SetRegion(region string) {
	req.Region = region
}

// 网段管理
// +k8s:deepcopy-gen=true
// +deepequal-gen=true
type Subnet struct {
	Name       string `json:"name,omitempty"  binding:"required"`
	Region     string `json:"Region,omitempty"  binding:"required"`
	VPC        string `json:"vpc,omitempty"  binding:"required"`
	Purpose    string `json:"purpose,omitempty"  binding:"required"`
	ISP        string `json:"isp,omitempty"`
	IPVersion  int    `json:"ipVersion,omitemptyn"  binding:"required"`
	RangeStart string `json:"rangeStart,omitempty"`
	RangeEnd   string `json:"rangeEnd,omitempty"`
	Excludes   string `json:"excludes,omitempty"`
	CIDR       string `json:"cidr,omitempty"  binding:"required"`
	Gateway    string `json:"gateway,omitempty"`
	NatGW      string `json:"natGW,omitempty"`
	VlanID     int    `json:"vlanID,omitempty"`
	Priority   int    `json:"priority,omitempty"`
	Enable     bool   `json:"enable,omitempty"`
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type SubnetUpdateRequest struct {
	Subnet
	VlanID   string `json:"vlanID,omitempty"`
	Enable   string `json:"enable,omitempty"`
	Priority string `json:"priority,omitempty"`
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type ListSubnetRequest struct {
	Region     string `json:"Region,omitempty"`
	VPC        string `json:"vpc,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
	ISP        string `json:"isp,omitempty"`
	IPVersion  int    `json:"ipVersion,omitempty"`
	MustEnable bool   `json:"mustEnable,omitempty"`
	WithShared bool   `json:"withShared,omitempty"`
}

func (req *ListSubnetRequest) SetRegion(region string) {
	req.Region = region
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type SubnetResponse struct {
	ErrorResponse `json:",inline"`
	Subnet        Subnet `json:"subnet,omitempty"`
}

// +k8s:deepcopy-gen=false
// +deepequal-gen=false
type ListSubnetResponse struct {
	ErrorResponse `json:",inline"`
	Subnets       []*Subnet `json:"subnets,omitempty"`
}
