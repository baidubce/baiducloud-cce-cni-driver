# \EndpointApi

All URIs are relative to *https://localhost/v1*

Method | HTTP request | Description
------------- | ------------- | -------------
[**endpoint_extplugin_status_get**](EndpointApi.md#endpoint_extplugin_status_get) | **Get** /endpoint/extplugin/status | get external plugin status
[**endpoint_probe_put**](EndpointApi.md#endpoint_probe_put) | **Put** /endpoint/probe | create or update endpint probe


# **endpoint_extplugin_status_get**
> ::models::ExtFeatureData endpoint_extplugin_status_get(optional)
get external plugin status

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **optional** | **map[string]interface{}** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a map[string]interface{}.

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **owner** | **String**|  | 
 **container_id** | **String**| container id provider by cni | 

### Return type

[**::models::ExtFeatureData**](ExtFeatureData.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **endpoint_probe_put**
> ::models::EndpointProbeResponse endpoint_probe_put(optional)
create or update endpint probe

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **optional** | **map[string]interface{}** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a map[string]interface{}.

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **owner** | **String**|  | 
 **container_id** | **String**| container id provider by cni | 
 **netns** | **String**| netns provider by cni | 
 **ifname** | **String**| ifname provider by cni | 
 **cni_driver** | **String**|  | 

### Return type

[**::models::EndpointProbeResponse**](EndpointProbeResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

