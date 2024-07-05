# EndpointIdentifiers

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**container_id** | **String** | ID assigned by container runtime | [optional] [default to null]
**container_name** | **String** | Name assigned to container | [optional] [default to null]
**pod_name** | **String** | K8s pod for this endpoint(Deprecated, use K8sPodName and K8sNamespace instead) | [optional] [default to null]
**k8s_pod_name** | **String** | K8s pod name for this endpoint | [optional] [default to null]
**k8s_namespace** | **String** | K8s namespace for this endpoint | [optional] [default to null]
**k8s_object_id** | **String** | K8s object id to indentifier a unique object | [optional] [default to null]
**external_identifier** | **String** | External network ID, such as the ID of the ENI occupied by the container | [optional] [default to null]
**netns** | **String** | netns use by CNI | [optional] [default to null]
**cnidriver** | **String** | device driver name | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


