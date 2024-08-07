# Rust API client for cni-common

CCE

## Overview
This API client was generated by the [swagger-codegen](https://github.com/swagger-api/swagger-codegen) project.  By using the [swagger-spec](https://github.com/swagger-api/swagger-spec) from a remote server, you can easily generate an API client.

- API version: v1beta
- Package version: 1.0.0
- Build package: io.swagger.codegen.languages.RustClientCodegen

## Installation
Put the package under your project folder and add the following in import:
```
    "./cni-common"
```

## Documentation for API Endpoints

All URIs are relative to *https://localhost/v1*

Class | Method | HTTP request | Description
------------ | ------------- | ------------- | -------------
*DaemonApi* | [**healthz_get**](docs/DaemonApi.md#healthz_get) | **Get** /healthz | Get health of CCE daemon
*EndpointApi* | [**endpoint_extplugin_status_get**](docs/EndpointApi.md#endpoint_extplugin_status_get) | **Get** /endpoint/extplugin/status | get external plugin status
*EndpointApi* | [**endpoint_probe_put**](docs/EndpointApi.md#endpoint_probe_put) | **Put** /endpoint/probe | create or update endpint probe
*EniApi* | [**eni_delete**](docs/EniApi.md#eni_delete) | **Delete** /eni | Release an allocated IP address for exclusive ENI
*EniApi* | [**eni_post**](docs/EniApi.md#eni_post) | **Post** /eni | Allocate an IP address for exclusive ENI
*IpamApi* | [**ipam_ip_delete**](docs/IpamApi.md#ipam_ip_delete) | **Delete** /ipam/{ip} | Release an allocated IP address
*IpamApi* | [**ipam_ip_post**](docs/IpamApi.md#ipam_ip_post) | **Post** /ipam/{ip} | Allocate an IP address
*IpamApi* | [**ipam_post**](docs/IpamApi.md#ipam_post) | **Post** /ipam | Allocate an IP address
*MetricsApi* | [**metrics_get**](docs/MetricsApi.md#metrics_get) | **Get** /metrics/ | Retrieve CCE metrics


## Documentation For Models

 - [Address](docs/Address.md)
 - [AddressPair](docs/AddressPair.md)
 - [BandwidthOption](docs/BandwidthOption.md)
 - [ControllerStatus](docs/ControllerStatus.md)
 - [ControllerStatusConfiguration](docs/ControllerStatusConfiguration.md)
 - [ControllerStatusStatus](docs/ControllerStatusStatus.md)
 - [ControllerStatuses](docs/ControllerStatuses.md)
 - [DatapathMode](docs/DatapathMode.md)
 - [EndpointIdentifiers](docs/EndpointIdentifiers.md)
 - [EndpointProbeResponse](docs/EndpointProbeResponse.md)
 - [EndpointState](docs/EndpointState.md)
 - [EndpointStatusChange](docs/EndpointStatusChange.md)
 - [EndpointStatusLog](docs/EndpointStatusLog.md)
 - [Eni](docs/Eni.md)
 - [Error](docs/Error.md)
 - [ExtFeatureData](docs/ExtFeatureData.md)
 - [IpamAddressResponse](docs/IpamAddressResponse.md)
 - [IpamResponse](docs/IpamResponse.md)
 - [Metric](docs/Metric.md)
 - [NodeAddressing](docs/NodeAddressing.md)
 - [NodeAddressingElement](docs/NodeAddressingElement.md)
 - [PrivateIp](docs/PrivateIp.md)
 - [Status](docs/Status.md)
 - [StatusResponse](docs/StatusResponse.md)


## Documentation For Authorization
 Endpoints do not require authorization.


## Author



