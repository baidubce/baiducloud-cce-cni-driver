---
title: ENI 管理器
description: "plugins/enim/README.md"
---

enim 可以为容器申请一个独立的ENI，并提供ENI的IPv4/IPv6地址和默认路由配置信息。

## Overview

enim是一个符合CNI标准IPAM规范的的CNI插件，它上承如`host-device`独立设备插件，下启`cce-cni-agent`的ENI管理接口。
提供申请独占ENI和释放独占ENI的功能，并提供通用CNI插件。

目前enim在每次调用时每个Owner（一般指Pod的namespace/name）只允许申请一个ENI。对ENI设备做复用管理，即ADD方法把设备加入到
容器的网络命名空间，DEL方法把设备移回初始网络命名空间，并不会真正的卸载设备。

## Example configurations

下面是一个最简单的示例配置


```json
{
	"ipam": {
		"type": "enim",
	}
}
```


通过下面的命令行测试:

```bash
$ echo '{ "cniVersion": "1.0.10", "name": "examplenet", "ipam": { "type": "enim"} }' | CNI_COMMAND=ADD CNI_CONTAINERID=example CNI_NETNS=/dev/null CNI_IFNAME=dummy0 CNI_PATH=. ./host-local

```

```json
{
    "ips": [
        {
            "interface": "10",
            "address": "203.0.113.2/24",
            "gateway": "203.0.113.1"
        },
        {
            "interface": "10",
            "address": "2001:db8:1::2/64",
            "gateway": "2001:db8:1::1"
        }
    ],
    "interfaces":[
        {
            "mac": "fa:26:00:03:3d:15",
        }
    ]
}
```

* `ips.interface`: ENI设备在linux网络设备中的ID，供上层设备快速查找设备使用。
* `ips.address`: ENI 设备IP地址(IPv4/IPv6)，地址使用CIDR格式表示
* `ips.gateway`: ENI设备的默认网关
* `interfaces.mac`: ENI 设备的MAC地址


## Network configuration reference

* `type` (string, required): "enim".
* `routes` (string, optional): list of routes to add to the container namespace. Each route is a dictionary with "dst" and optional "gw" fields. If "gw" is omitted, value of "gateway" will be used.

## Supported arguments
The following [CNI_ARGS](https://github.com/containernetworking/cni/blob/master/SPEC.md#parameters) are supported:

* `ip`: request a specific IP address from a subnet.

The following [args conventions](https://github.com/containernetworking/cni/blob/master/CONVENTIONS.md) are supported:

* `ips` (array of strings): A list of custom IPs to attempt to allocate

The following [Capability Args](https://github.com/containernetworking/cni/blob/master/CONVENTIONS.md) are supported:

* `ipRanges`: The exact same as the `ranges` array - a list of address pools

### Custom IP allocation
每次调用ADD时，都会尝试为接口新建ENI（如果存在），也就是说：
如果调用ADD时，旧的容器还未调用DEL，则会新建一个ENI。

旧容器的使用的ENI则会进入回收流程：
* 等待CNI调用DEL触发回收
* 由enim定时触发回收