# StatusResponse

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**cce** | [***::models::Status**](Status.md) | Status of CCE daemon | [optional] [default to null]
**container_runtime** | [***::models::Status**](Status.md) | Status of local container runtime | [optional] [default to null]
**controllers** | [***::models::ControllerStatuses**](ControllerStatuses.md) | Status of all endpoint controllers | [optional] [default to null]
**stale** | **::std::collections::HashMap<String, String>** | List of stale information in the status | [optional] [default to null]
**client_id** | **String** | When supported by the API, this client ID should be used by the client when making another request to the server. See for example \&quot;/cluster/nodes\&quot;.  | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


