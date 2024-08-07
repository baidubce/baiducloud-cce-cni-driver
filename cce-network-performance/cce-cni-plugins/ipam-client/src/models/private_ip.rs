/*
 * Cce API
 *
 * CCE
 *
 * OpenAPI spec version: v1beta
 *
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 */

/// PrivateIp : VPC IP address  +k8s:deepcopy-gen=true

#[allow(unused_imports)]
use serde_json::Value;

#[derive(Debug, Serialize, Deserialize)]
pub struct PrivateIp {
    #[serde(rename = "publicIPAddress")]
    public_ip_address: Option<String>,
    #[serde(rename = "primary")]
    primary: Option<bool>,
    #[serde(rename = "privateIPAddress")]
    private_ip_address: Option<String>,
    /// SubnetID subnet id of  private ip assigned with this. When allocating IP across subnets, the subnet of private IP may be different from ENI
    #[serde(rename = "subnetID")]
    subnet_id: Option<String>,
    #[serde(rename = "gatewayIP")]
    gateway_ip: Option<String>,
    #[serde(rename = "cidrAddress")]
    cidr_address: Option<String>,
}

impl PrivateIp {
    /// VPC IP address  +k8s:deepcopy-gen=true
    pub fn new() -> PrivateIp {
        PrivateIp {
            public_ip_address: None,
            primary: None,
            private_ip_address: None,
            subnet_id: None,
            gateway_ip: None,
            cidr_address: None,
        }
    }

    pub fn set_public_ip_address(&mut self, public_ip_address: String) {
        self.public_ip_address = Some(public_ip_address);
    }

    pub fn with_public_ip_address(mut self, public_ip_address: String) -> PrivateIp {
        self.public_ip_address = Some(public_ip_address);
        self
    }

    pub fn public_ip_address(&self) -> Option<&String> {
        self.public_ip_address.as_ref()
    }

    pub fn reset_public_ip_address(&mut self) {
        self.public_ip_address = None;
    }

    pub fn set_primary(&mut self, primary: bool) {
        self.primary = Some(primary);
    }

    pub fn with_primary(mut self, primary: bool) -> PrivateIp {
        self.primary = Some(primary);
        self
    }

    pub fn primary(&self) -> Option<&bool> {
        self.primary.as_ref()
    }

    pub fn reset_primary(&mut self) {
        self.primary = None;
    }

    pub fn set_private_ip_address(&mut self, private_ip_address: String) {
        self.private_ip_address = Some(private_ip_address);
    }

    pub fn with_private_ip_address(mut self, private_ip_address: String) -> PrivateIp {
        self.private_ip_address = Some(private_ip_address);
        self
    }

    pub fn private_ip_address(&self) -> Option<&String> {
        self.private_ip_address.as_ref()
    }

    pub fn reset_private_ip_address(&mut self) {
        self.private_ip_address = None;
    }

    pub fn set_subnet_id(&mut self, subnet_id: String) {
        self.subnet_id = Some(subnet_id);
    }

    pub fn with_subnet_id(mut self, subnet_id: String) -> PrivateIp {
        self.subnet_id = Some(subnet_id);
        self
    }

    pub fn subnet_id(&self) -> Option<&String> {
        self.subnet_id.as_ref()
    }

    pub fn reset_subnet_id(&mut self) {
        self.subnet_id = None;
    }

    pub fn set_gateway_ip(&mut self, gateway_ip: String) {
        self.gateway_ip = Some(gateway_ip);
    }

    pub fn with_gateway_ip(mut self, gateway_ip: String) -> PrivateIp {
        self.gateway_ip = Some(gateway_ip);
        self
    }

    pub fn gateway_ip(&self) -> Option<&String> {
        self.gateway_ip.as_ref()
    }

    pub fn reset_gateway_ip(&mut self) {
        self.gateway_ip = None;
    }

    pub fn set_cidr_address(&mut self, cidr_address: String) {
        self.cidr_address = Some(cidr_address);
    }

    pub fn with_cidr_address(mut self, cidr_address: String) -> PrivateIp {
        self.cidr_address = Some(cidr_address);
        self
    }

    pub fn cidr_address(&self) -> Option<&String> {
        self.cidr_address.as_ref()
    }

    pub fn reset_cidr_address(&mut self) {
        self.cidr_address = None;
    }
}
