/*
 * Cce API
 *
 * CCE
 *
 * OpenAPI spec version: v1beta
 *
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 */

/// Eni : eni definition  +k8s:deepcopy-gen=true

#[allow(unused_imports)]
use serde_json::Value;

#[derive(Debug, Serialize, Deserialize)]
pub struct Eni {
    /// ID unique identifier available for the resource
    #[serde(rename = "id")]
    id: Option<String>,
    /// The name of eni was created by CCE and format as {clusterID}-{instance_name}-{randmon}
    #[serde(rename = "name")]
    name: Option<String>,
    /// ZoneName zone name of eni instance
    #[serde(rename = "zoneName")]
    zone_name: Option<String>,
    /// InstanceID insance ID of node resource, like ID of BCC. This field represents the node name that ENI expects to attached. Example: i-wWANcEYK
    #[serde(rename = "instanceID")]
    instance_id: Option<String>,
    /// VPCID vpc id of eni instance In scenarios where ENI is used across VPCs (such as BCI), the VPC ID of ENI may be different from that of the cluster Example: vpc-8nh1ks7a55a2
    #[serde(rename = "vpcID")]
    vpc_id: Option<String>,
    #[serde(rename = "description")]
    description: Option<String>,
    /// MacAddress mac address of eni instance After the ENI is attached to the VM, the ENI device should be found in the VM through this mac address Example: fa:26:00:0d:51:c7
    #[serde(rename = "macAddress")]
    mac_address: Option<String>,
    /// SubnetID subnet id of eni instance In scenarios where ENI is used across Subnet (such as PodSubnetTopologySpread), the subnet ID of ENI may be different from secondry IP of the ENI. Example: sbn-na1y2xryjyf3
    #[serde(rename = "subnetID")]
    subnet_id: Option<String>,
    /// SecurityGroupIds list of security group IDs An ENI should have at least one default security group Example: [\"g-xpy9eitxhfib\"]
    #[serde(rename = "securityGroupIds")]
    security_group_ids: Option<Vec<String>>,
    #[serde(rename = "enterpriseSecurityGroupIds")]
    enterprise_security_group_ids: Option<Vec<String>>,
    #[serde(rename = "privateIPSet")]
    private_ip_set: Option<Vec<::models::PrivateIp>>,
    #[serde(rename = "ipv6PrivateIPSet")]
    ipv6_private_ip_set: Option<Vec<::models::PrivateIp>>,
}

impl Eni {
    /// eni definition  +k8s:deepcopy-gen=true
    pub fn new() -> Eni {
        Eni {
            id: None,
            name: None,
            zone_name: None,
            instance_id: None,
            vpc_id: None,
            description: None,
            mac_address: None,
            subnet_id: None,
            security_group_ids: None,
            enterprise_security_group_ids: None,
            private_ip_set: None,
            ipv6_private_ip_set: None,
        }
    }

    pub fn set_id(&mut self, id: String) {
        self.id = Some(id);
    }

    pub fn with_id(mut self, id: String) -> Eni {
        self.id = Some(id);
        self
    }

    pub fn id(&self) -> Option<&String> {
        self.id.as_ref()
    }

    pub fn reset_id(&mut self) {
        self.id = None;
    }

    pub fn set_name(&mut self, name: String) {
        self.name = Some(name);
    }

    pub fn with_name(mut self, name: String) -> Eni {
        self.name = Some(name);
        self
    }

    pub fn name(&self) -> Option<&String> {
        self.name.as_ref()
    }

    pub fn reset_name(&mut self) {
        self.name = None;
    }

    pub fn set_zone_name(&mut self, zone_name: String) {
        self.zone_name = Some(zone_name);
    }

    pub fn with_zone_name(mut self, zone_name: String) -> Eni {
        self.zone_name = Some(zone_name);
        self
    }

    pub fn zone_name(&self) -> Option<&String> {
        self.zone_name.as_ref()
    }

    pub fn reset_zone_name(&mut self) {
        self.zone_name = None;
    }

    pub fn set_instance_id(&mut self, instance_id: String) {
        self.instance_id = Some(instance_id);
    }

    pub fn with_instance_id(mut self, instance_id: String) -> Eni {
        self.instance_id = Some(instance_id);
        self
    }

    pub fn instance_id(&self) -> Option<&String> {
        self.instance_id.as_ref()
    }

    pub fn reset_instance_id(&mut self) {
        self.instance_id = None;
    }

    pub fn set_vpc_id(&mut self, vpc_id: String) {
        self.vpc_id = Some(vpc_id);
    }

    pub fn with_vpc_id(mut self, vpc_id: String) -> Eni {
        self.vpc_id = Some(vpc_id);
        self
    }

    pub fn vpc_id(&self) -> Option<&String> {
        self.vpc_id.as_ref()
    }

    pub fn reset_vpc_id(&mut self) {
        self.vpc_id = None;
    }

    pub fn set_description(&mut self, description: String) {
        self.description = Some(description);
    }

    pub fn with_description(mut self, description: String) -> Eni {
        self.description = Some(description);
        self
    }

    pub fn description(&self) -> Option<&String> {
        self.description.as_ref()
    }

    pub fn reset_description(&mut self) {
        self.description = None;
    }

    pub fn set_mac_address(&mut self, mac_address: String) {
        self.mac_address = Some(mac_address);
    }

    pub fn with_mac_address(mut self, mac_address: String) -> Eni {
        self.mac_address = Some(mac_address);
        self
    }

    pub fn mac_address(&self) -> Option<&String> {
        self.mac_address.as_ref()
    }

    pub fn reset_mac_address(&mut self) {
        self.mac_address = None;
    }

    pub fn set_subnet_id(&mut self, subnet_id: String) {
        self.subnet_id = Some(subnet_id);
    }

    pub fn with_subnet_id(mut self, subnet_id: String) -> Eni {
        self.subnet_id = Some(subnet_id);
        self
    }

    pub fn subnet_id(&self) -> Option<&String> {
        self.subnet_id.as_ref()
    }

    pub fn reset_subnet_id(&mut self) {
        self.subnet_id = None;
    }

    pub fn set_security_group_ids(&mut self, security_group_ids: Vec<String>) {
        self.security_group_ids = Some(security_group_ids);
    }

    pub fn with_security_group_ids(mut self, security_group_ids: Vec<String>) -> Eni {
        self.security_group_ids = Some(security_group_ids);
        self
    }

    pub fn security_group_ids(&self) -> Option<&Vec<String>> {
        self.security_group_ids.as_ref()
    }

    pub fn reset_security_group_ids(&mut self) {
        self.security_group_ids = None;
    }

    pub fn set_enterprise_security_group_ids(
        &mut self,
        enterprise_security_group_ids: Vec<String>,
    ) {
        self.enterprise_security_group_ids = Some(enterprise_security_group_ids);
    }

    pub fn with_enterprise_security_group_ids(
        mut self,
        enterprise_security_group_ids: Vec<String>,
    ) -> Eni {
        self.enterprise_security_group_ids = Some(enterprise_security_group_ids);
        self
    }

    pub fn enterprise_security_group_ids(&self) -> Option<&Vec<String>> {
        self.enterprise_security_group_ids.as_ref()
    }

    pub fn reset_enterprise_security_group_ids(&mut self) {
        self.enterprise_security_group_ids = None;
    }

    pub fn set_private_ip_set(&mut self, private_ip_set: Vec<::models::PrivateIp>) {
        self.private_ip_set = Some(private_ip_set);
    }

    pub fn with_private_ip_set(mut self, private_ip_set: Vec<::models::PrivateIp>) -> Eni {
        self.private_ip_set = Some(private_ip_set);
        self
    }

    pub fn private_ip_set(&self) -> Option<&Vec<::models::PrivateIp>> {
        self.private_ip_set.as_ref()
    }

    pub fn reset_private_ip_set(&mut self) {
        self.private_ip_set = None;
    }

    pub fn set_ipv6_private_ip_set(&mut self, ipv6_private_ip_set: Vec<::models::PrivateIp>) {
        self.ipv6_private_ip_set = Some(ipv6_private_ip_set);
    }

    pub fn with_ipv6_private_ip_set(
        mut self,
        ipv6_private_ip_set: Vec<::models::PrivateIp>,
    ) -> Eni {
        self.ipv6_private_ip_set = Some(ipv6_private_ip_set);
        self
    }

    pub fn ipv6_private_ip_set(&self) -> Option<&Vec<::models::PrivateIp>> {
        self.ipv6_private_ip_set.as_ref()
    }

    pub fn reset_ipv6_private_ip_set(&mut self) {
        self.ipv6_private_ip_set = None;
    }
}
