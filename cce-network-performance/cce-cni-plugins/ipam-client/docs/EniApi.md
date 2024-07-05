# \EniApi

All URIs are relative to *https://localhost/v1*

Method | HTTP request | Description
------------- | ------------- | -------------
[**eni_delete**](EniApi.md#eni_delete) | **Delete** /eni | Release an allocated IP address for exclusive ENI
[**eni_post**](EniApi.md#eni_post) | **Post** /eni | Allocate an IP address for exclusive ENI


# **eni_delete**
> eni_delete(optional)
Release an allocated IP address for exclusive ENI

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

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **eni_post**
> ::models::IpamResponse eni_post(optional)
Allocate an IP address for exclusive ENI

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **optional** | **map[string]interface{}** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a map[string]interface{}.

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **eni** | [**Eni**](Eni.md)| Expectations when applying for ENI | 
 **owner** | **String**|  | 
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

