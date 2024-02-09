---
title: endpoint-probe 
description: "plugins/main/endpoint-probe/README.md"
date: 2020-11-02
toc: true
draft: false
weight: 200
---

# endpoint-probe
cce 定制后置扩展插件，定制功能如下：
1. 在完成所有 CNI 插件后，对 endpoint 发起探测，查看需要后置检查的所有扩展能力。
2. 当前支持的扩展能力有 带宽管理、网络Qos管理。

## 概述
ptp插件通过使用veth设备在容器和主机之间创建点对点链接。
veth对的一端放置在容器内，另一端位于主机上。
host-local IPAM插件可用于为容器分配IP地址。
容器接口的流量将通过主机的接口进行路由。

说明1: 为兼容Calico Felix NetworkPolicy, 当前cptp插件创建host veth时仍按照V1的11字符命名规范. 后续会改为与原始ptp相同的8字符命名规范.

## 配置示例

```json
{
	"name": "mynet",
	"type": "cnext",
	"capabilities": {
		"bindwidth": {

		}
	}
}
```

## Network configuration reference

* `name` (string, required): the name of the network
* `type` (string, required): "ptp"
* `ipMasq` (boolean, optional): set up IP Masquerade on the host for traffic originating from ip of this network and destined outside of this network. Defaults to false.
* `mtu` (integer, optional): explicitly set MTU to the specified value. Defaults to value chosen by the kernel.
* `ipam` (dictionary, required): IPAM configuration to be used for this network.
* `dns` (dictionary, optional): DNS information to return as described in the [Result](https://github.com/containernetworking/cni/blob/master/SPEC.md#result).

## 原始代码地址
https://github.com/containernetworking/plugins/blob/4a6147a1552064af80b4f7567b30c5174153c62a/plugins/main/ptp/ptp.go