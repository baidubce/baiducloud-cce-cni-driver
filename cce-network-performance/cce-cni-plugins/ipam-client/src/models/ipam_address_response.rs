/*
 * Cce API
 *
 * CCE
 *
 * OpenAPI spec version: v1beta
 *
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 */

/// IpamAddressResponse : IPAM configuration of an individual address family

#[allow(unused_imports)]
use serde_json::Value;

#[derive(Debug, Serialize, Deserialize)]
pub struct IpamAddressResponse {
    /// Allocated IP for endpoint
    #[serde(rename = "ip")]
    ip: Option<String>,
    /// IP of gateway
    #[serde(rename = "gateway")]
    gateway: Option<String>,
    /// List of CIDRs out of which IPs are allocated
    #[serde(rename = "cidrs")]
    cidrs: Option<Vec<String>>,
    /// MAC of master interface if address is a slave/secondary of a master interface
    #[serde(rename = "master-mac")]
    master_mac: Option<String>,
    /// The UUID for the expiration timer. Set when expiration has been enabled while allocating.
    #[serde(rename = "expiration-uuid")]
    expiration_uuid: Option<String>,
    /// InterfaceNumber is a field for generically identifying an interface. This is only useful in ENI mode.
    #[serde(rename = "interface-number")]
    interface_number: Option<String>,
}

impl IpamAddressResponse {
    /// IPAM configuration of an individual address family
    pub fn new() -> IpamAddressResponse {
        IpamAddressResponse {
            ip: None,
            gateway: None,
            cidrs: None,
            master_mac: None,
            expiration_uuid: None,
            interface_number: None,
        }
    }

    pub fn set_ip(&mut self, ip: String) {
        self.ip = Some(ip);
    }

    pub fn with_ip(mut self, ip: String) -> IpamAddressResponse {
        self.ip = Some(ip);
        self
    }

    pub fn ip(&self) -> Option<&String> {
        self.ip.as_ref()
    }

    pub fn reset_ip(&mut self) {
        self.ip = None;
    }

    pub fn set_gateway(&mut self, gateway: String) {
        self.gateway = Some(gateway);
    }

    pub fn with_gateway(mut self, gateway: String) -> IpamAddressResponse {
        self.gateway = Some(gateway);
        self
    }

    pub fn gateway(&self) -> Option<&String> {
        self.gateway.as_ref()
    }

    pub fn reset_gateway(&mut self) {
        self.gateway = None;
    }

    pub fn set_cidrs(&mut self, cidrs: Vec<String>) {
        self.cidrs = Some(cidrs);
    }

    pub fn with_cidrs(mut self, cidrs: Vec<String>) -> IpamAddressResponse {
        self.cidrs = Some(cidrs);
        self
    }

    pub fn cidrs(&self) -> Option<&Vec<String>> {
        self.cidrs.as_ref()
    }

    pub fn reset_cidrs(&mut self) {
        self.cidrs = None;
    }

    pub fn set_master_mac(&mut self, master_mac: String) {
        self.master_mac = Some(master_mac);
    }

    pub fn with_master_mac(mut self, master_mac: String) -> IpamAddressResponse {
        self.master_mac = Some(master_mac);
        self
    }

    pub fn master_mac(&self) -> Option<&String> {
        self.master_mac.as_ref()
    }

    pub fn reset_master_mac(&mut self) {
        self.master_mac = None;
    }

    pub fn set_expiration_uuid(&mut self, expiration_uuid: String) {
        self.expiration_uuid = Some(expiration_uuid);
    }

    pub fn with_expiration_uuid(mut self, expiration_uuid: String) -> IpamAddressResponse {
        self.expiration_uuid = Some(expiration_uuid);
        self
    }

    pub fn expiration_uuid(&self) -> Option<&String> {
        self.expiration_uuid.as_ref()
    }

    pub fn reset_expiration_uuid(&mut self) {
        self.expiration_uuid = None;
    }

    pub fn set_interface_number(&mut self, interface_number: String) {
        self.interface_number = Some(interface_number);
    }

    pub fn with_interface_number(mut self, interface_number: String) -> IpamAddressResponse {
        self.interface_number = Some(interface_number);
        self
    }

    pub fn interface_number(&self) -> Option<&String> {
        self.interface_number.as_ref()
    }

    pub fn reset_interface_number(&mut self) {
        self.interface_number = None;
    }
}
