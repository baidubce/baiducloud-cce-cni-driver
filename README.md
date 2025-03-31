# Baidu Cloud CNI Plugin
## Introduction
Baidu Cloud CNI plugin implement an interface between CNI enabled Container Orchestrator and Baidu Cloud Network Infrastructure.

## Getting Started
These instructions will get you a copy of the project up and running on your environment for development and testing purposes. See installing for notes on how to deploy the project on a Baidu Cloud CCE cluster.

## Prerequisites
A healthy CCE kubernetes cluster. See documents for creating a CCE cluster.

## Components
There are 2 components:
* cce-network-v2: The v2 version of the container network has been optimized for the internal state and availability of the network. Provides network modes such as `VPC-ENI`and `VPC-Route` that are more suitable for BCE Cloud.
* eip-operator: Assign EIP directly to Pod to help Pod connect to the Internet directly

## design
### VPC-ENI
[VPC-ENI](docs/vpc-eni/vpc-eni.md) provides a container network originating from BCE Cloud, where container addresses and node addresses use the same network segment.


### VPC-Route
The [VPC-Route](docs/vpc-route/vpc-route.md)) mode utilizes the custom routing rule capability provided by VPC to make the virtual network address segment of the container accessible to the entire VPC.

## Contributing
Please go through CNI Spec to get some basic understanding of CNI driver before you start.

## Requirements
* Recommended linux kervel version >= 5.10
* Golang version >= 1.21
* k8s version >= 1.20

## Issues
Please create an issue in issue list.
Contact Committers/Owners for further discussion if needed.

