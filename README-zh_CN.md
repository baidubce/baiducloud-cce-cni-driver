# 百度云 CNI 插件

[English](./README.md) | 简体中文

## 插件介绍

百度云 CNI 插件实现了在 Kubernetes 中对百度云弹性网卡、辅助 IP 的管理与使用。当前 CNI实现基于 CNI spec 0.4.0版本，支持 K8S 1.16 及以上。

## 快速开始

本小节介绍如何在一个百度云 [CCE](https://cloud.baidu.com/product/cce.html) 集群中快速部署 CNI 插件。

### 前置条件

需要具备一个可用的百度云 CCE 集群。见[创建一个 CCE 集群](https://cloud.baidu.com/doc/CCE/s/zjxpoqohb).

### 特性

从单节点内容器连通性的角度来看, 百度云 CNI 插件支持两种模式:
- veth (适合所有版本的操作系统镜像)
- ipvlan (需要内核版本 >= 4.9, 例如 ubuntu16/18 and centos8+)

从跨节点容器连通性的角度来看, 百度云 CNI 插件支持三种模式:
- VPC 路由模式
- 弹性网卡辅助 IP 模式 (仅支持 BCC)
- BBC 主网卡辅助 IP 模式

### 组件

总共有三个组件:

- CNI 插件, 连接容器和宿主机的网络栈
- Node Agent, 在每个节点运行的守护进程，负责:
  - 维护 `/etc/cni/net.d/` 目录下的 CNI 配置文件
  - 安装 CNI 插件二进制到 `/opt/cni/bin/` 目录
  - 配置弹性网卡
  - 维护 VPC 路由
- ENI IPAM, 中心化的 IP 分配组件，支持:
  - 创建和绑定弹性网卡
  - 为 Pod 分配辅助 IP

<img src="./docs/images/cni-components.png" />

### 部署

在 `build/yamls/cce-cni-driver/values.yaml` 填入正确的信息，然后执行

```
make charts VALUES=build/yamls/cce-cni-driver/values.yaml | kubectl apply -f -
```

假设我们有个在 `bj` 地域的 CCE 集群，集群 ID 是 `cce-xxxxx`，集群所属 VPC 是 `vpc-yyyyy`


样例的 `values.yaml` 如下:

#### VPC 路由模式
```yaml
CNIMode: vpc-route-auto-detect
Region: bj
ClusterID: cce-xxxxx
VPCID: vpc-yyyyy
ContainerNetworkCIDRIPv4: # cluster container cidr
CCEGatewayEndpoint: cce-gateway.bj.baidubce.com
BCCEndpoint: bcc.bj.baidubce.com
BBCEndpoint: bbc.bj.baidubce.com
ServiceCIDR: # cluster service cidr
CCECNIImage: registry.baidubce.com/cce-plugin-pro/cce-cni:v1.3.0

# Route Controller
EnableVPCRoute: true
EnableStaticRoute: false
```

#### 弹性网卡辅助 IP 模式

```yaml
CNIMode: vpc-secondary-ip-auto-detect
Region: bj
ClusterID: cce-xxxxx
VPCID: vpc-yyyyy
ENISubnetList:
  - sbn-a
  - sbn-b
SecurityGroupList:
  - g-bwswsr8fbjb4
CCEGatewayEndpoint: cce-gateway.bj.baidubce.com
BCCEndpoint: bcc.bj.baidubce.com
BBCEndpoint: bbc.bj.baidubce.com
ServiceCIDR: # cluster service cidr
CCECNIImage: registry.baidubce.com/cce-plugin-pro/cce-cni:v1.3.0
```

#### BBC 主网卡辅助 IP 模式

```yaml
CNIMode: bbc-vpc-secondary-ip-auto-detect
Region: bj
ClusterID: cce-xxxxx
VPCID: vpc-yyyyy
CCEGatewayEndpoint: cce-gateway.bj.baidubce.com
BCCEndpoint: bcc.bj.baidubce.com
BBCEndpoint: bbc.bj.baidubce.com
ServiceCIDR: # cluster service cidr
CCECNIImage: registry.baidubce.com/cce-plugin-pro/cce-cni:v1.3.0
```


## 测试

### 单元测试

```
make test
```

## 如何贡献

请先阅读[CNI Spec](https://github.com/containernetworking/cni/blob/master/SPEC.md) ，了解 CNI 的基本原理和开发指导。

### 环境

* Golang 1.13.+
* Docker 17.05+ 用于镜像发布

### 依赖管理

Go module

### 镜像构建

```
export GO111MODULE=on
make build
make cni-image
```

### Issues

接受的 Issues 包括：

* 需求与建议。
* Bug。

### 维护者

* 主要维护者：: chenyaqi01@baidu.com, jichao04@baidu.com

### 讨论

* Issue 列表
* 如流群：1586317