# Eni

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **String** | ID unique identifier available for the resource | [optional] [default to null]
**name** | **String** | The name of eni was created by CCE and format as {clusterID}-{instance_name}-{randmon} | [optional] [default to null]
**zone_name** | **String** | ZoneName zone name of eni instance | [optional] [default to null]
**instance_id** | **String** | InstanceID insance ID of node resource, like ID of BCC. This field represents the node name that ENI expects to attached. Example: i-wWANcEYK  | [optional] [default to null]
**vpc_id** | **String** | VPCID vpc id of eni instance In scenarios where ENI is used across VPCs (such as BCI), the VPC ID of ENI may be different from that of the cluster Example: vpc-8nh1ks7a55a2  | [optional] [default to null]
**description** | **String** |  | [optional] [default to null]
**mac_address** | **String** | MacAddress mac address of eni instance After the ENI is attached to the VM, the ENI device should be found in the VM through this mac address Example: fa:26:00:0d:51:c7  | [optional] [default to null]
**subnet_id** | **String** | SubnetID subnet id of eni instance In scenarios where ENI is used across Subnet (such as PodSubnetTopologySpread), the subnet ID of ENI may be different from secondry IP of the ENI. Example: sbn-na1y2xryjyf3  | [optional] [default to null]
**security_group_ids** | **Vec<String>** | SecurityGroupIds list of security group IDs An ENI should have at least one default security group Example: [\&quot;g-xpy9eitxhfib\&quot;]  | [optional] [default to null]
**enterprise_security_group_ids** | **Vec<String>** |  | [optional] [default to null]
**private_ip_set** | [**Vec<::models::PrivateIp>**](PrivateIP.md) |  | [optional] [default to null]
**ipv6_private_ip_set** | [**Vec<::models::PrivateIp>**](PrivateIP.md) |  | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


