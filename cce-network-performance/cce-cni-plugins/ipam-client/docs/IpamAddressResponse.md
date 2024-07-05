# IpamAddressResponse

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**ip** | **String** | Allocated IP for endpoint | [optional] [default to null]
**gateway** | **String** | IP of gateway | [optional] [default to null]
**cidrs** | **Vec<String>** | List of CIDRs out of which IPs are allocated | [optional] [default to null]
**master_mac** | **String** | MAC of master interface if address is a slave/secondary of a master interface | [optional] [default to null]
**expiration_uuid** | **String** | The UUID for the expiration timer. Set when expiration has been enabled while allocating.  | [optional] [default to null]
**interface_number** | **String** | InterfaceNumber is a field for generically identifying an interface. This is only useful in ENI mode.  | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


