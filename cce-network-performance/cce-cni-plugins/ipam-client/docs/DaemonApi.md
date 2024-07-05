# \DaemonApi

All URIs are relative to *https://localhost/v1*

Method | HTTP request | Description
------------- | ------------- | -------------
[**healthz_get**](DaemonApi.md#healthz_get) | **Get** /healthz | Get health of CCE daemon


# **healthz_get**
> ::models::StatusResponse healthz_get(optional)
Get health of CCE daemon

Returns health and status information of the CCE daemon and related components such as the local container runtime, connected datastore, Kubernetes integration and Hubble. 

### Required Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **optional** | **map[string]interface{}** | optional parameters | nil if no parameters

### Optional Parameters
Optional parameters are passed through a map[string]interface{}.

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **brief** | **bool**| Brief will return a brief representation of the CCE status.  | 

### Return type

[**::models::StatusResponse**](StatusResponse.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

