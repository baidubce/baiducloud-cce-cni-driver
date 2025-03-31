---
title: exclusive-rdma plugin
description: "plugins/main/exclusive-rdma/README.md"
date: 2024-10-16
toc: true
draft: true
weight: 200
---

# exclusive-rdma
cce 定制exclusive-rdma，定制功能如下：
1. 独占本机RDMA所有网卡给容器。

## 概述
exclusive-rdma插件通过将Node上的所有RDMA网卡setns进容器network namespace而进行独占RDMA网卡。
独占RDMA网卡后，Node上便不在能看到这些RDMA网卡，只能在容器中看到这些RDMA网卡。Node访问这些网卡通过容器的对应veth pair进行。
容器内的RDMA流量将直接通过容器内的RDMA网卡策略路由进行协议握手和通信。

## 配置示例

```json
{
	"type": "exclusive-rdma",
	"driver": "mlx5_core"
}
```

## Network configuration reference

* `type` (string, required): "exclusive-rdma"
* `driver` (string, optional): driver name of the RDMA device
