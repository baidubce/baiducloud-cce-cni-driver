# \IpamApi

All URIs are relative to *https://localhost/v1*

Method | HTTP request | Description
------------- | ------------- | -------------
[**ipam_ip_delete**](IpamApi.md#ipam_ip_delete) | **Delete** /ipam/{ip} | Release an allocated IP address
[**ipam_ip_post**](IpamApi.md#ipam_ip_post) | **Post** /ipam/{ip} | Allocate an IP address
[**ipam_post**](IpamApi.md#ipam_post) | **Post** /ipam | Allocate an IP address


# **ipam_ip_delete**
> ipam_ip_delete(ip, optional)
Release an allocated IP address

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
  **ip** | **String**| IP address or owner name | 
 **optional** | **map[string]interface{}** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a map[string]interface{}.

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ip** | **String**| IP address or owner name | 
 **owner** | **String**|  | 
 **container_id** | **String**| container id provider by cni | 
 **netns** | **String**| netns provider by cni | 
 **ifname** | **String**| ifname provider by cni | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **ipam_ip_post**
> ipam_ip_post(ip, optional)
Allocate an IP address

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
  **ip** | **String**| IP address | 
 **optional** | **map[string]interface{}** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a map[string]interface{}.

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **ip** | **String**| IP address | 
 **owner** | **String**|  | 
 **container_id** | **String**| container id provider by cni | 
 **netns** | **String**| netns provider by cni | 
 **ifname** | **String**| ifname provider by cni | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **ipam_post**
> ::models::IpamResponse ipam_post(optional)
Allocate an IP address

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **optional** | **map[string]interface{}** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a map[string]interface{}.

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **family** | **String**|  | 
 **owner** | **String**|  | 
 **expiration** | **bool**|  | 
 **container_id** | **String**| container id provider by cni | 
 **netns** | **String**| netns provider by cni | 
 **ifname** | **String**| ifname provider by cni | 

### Return type

[**::models::IpamResponse**](IPAMResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

