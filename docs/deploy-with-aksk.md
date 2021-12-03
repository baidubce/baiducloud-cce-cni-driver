# 百度云自建集群如何使用 CCE CNI

CCE CNI 是[百度云容器引擎CCE](https://cloud.baidu.com/product/cce.html) 的默认网络插件，但从设计上尽量与 CCE 的功能依赖解耦，因此针对部分用户使用百度云 BCC、BBC 自建 K8s 集群的场景，也可以部署使用 CCE CNI。

本文描述了如何在百度云自建集群部署 CCE CNI 组件的方法与步骤。


## 整体介绍

为了实现容器跨节点连通，CCE CNI 采用的方案有三种:
- VPC 路由
- 弹性网卡辅助 IP (仅 BCC 支持)
- 主网卡辅助 IP (仅二代 BBC 支持)

在此基础上，CCE 产品上支持的网络模式也主要分为三类:
- VPC网络
- VPC-CNI
- VPC-Hybrid

> 更具体的产品说明请参考 [CCE最佳实践之容器网络模式选择](https://cloud.baidu.com/doc/CCE/s/Rk5kokj7x)。

产品上的三类模式也对应这 CNI 组件内部定义的 [ContainerNetworkMode](../pkg/config/types/cnimode.go), 对应关系可总结为下表:

|  **产品模式**  | **ContainerNetworkMode** |
|:--------------:|:------------------------:|
|  **VPC网络**   |       vpc-route-*        |
|  **VPC-CNI**   |    vpc-secondary-ip-*    |
| **VPC-Hybrid** |  bbc-vpc-secondary-ip-*  |

## 前置检查

CCE CNI 对部署环境存在依赖，请确保已按照下述说明进行检查确认，否则 CNI 无法正常工作。

### K8s 集群层面

- 需要保证 node 中 spec.providerID 为 百度云机器的 instanceId；
- 需要保证 node 的 label 打上了 `beta.kubernetes.io/instance-type` 标签，并且是正确的机器类型 (`BBC` or `BCC`)


一个符合要求的 node 样例:
```bash
[root@cce-o3gh6vpz-t2gbo4se cce]# kubectl get node --show-labels 172.16.0.33
NAME          STATUS   ROLES    AGE    VERSION   LABELS
172.16.0.33   Ready    <none>   3d3h   v1.18.9   beta.kubernetes.io/arch=amd64,beta.kubernetes.io/instance-type=BBC,beta.kubernetes.io/os=linux
[root@cce-o3gh6vpz-t2gbo4se cce]# kubectl get node 172.16.0.33 -o yaml | grep "\sproviderID"
  providerID: i-4gLj6lfj
```


### 百度云层面

- 需要提供一对 ak/sk，且拥有 VPC 的读写权限以及 BCC/BBC 的读权限;

## 组件部署

对于自建集群的用户，部署 CCE CNI 组件使用 `helm chart` 渲染出部署 yaml。镜像版本选择[项目主页](../README.md)中的最新版本或者用户可以自行构建。

Values 文件以及配置项说明参考 [values.yaml](../build/yamls/cce-cni-driver/values.yaml)，无需指定的字段包括：
- CCEGatewayEndpoint
- IPAMScheduledToMaster

# 样例一

以用户使用 VPC 路由模式的方案为例，需要提供如下 Values 文件:

```
CNIMode: vpc-route-veth
Region: bj
VPCID: vpc-tvbxj4isix0h
ClusterID: baidu-k8s
BCCEndpoint: bcc.bj.baidubce.com
BBCEndpoint: bbc.bj.baidubce.com
CCECNIImage: registry.baidubce.com/cce-plugin-pro/cce-cni:v1.3.1

ServiceCIDR: 172.31.0.0/16

AccessKeyID: ak # 用户 ak (用户自建集群需指定)
SecretAccessKey: sk # 用户 sk (用户自建集群需指定)

EnableVPCRoute: true
EnableStaticRoute: false
```

# 样例二

以用户使用 BBC 主网卡辅助 IP 的方案为例，需要提供如下 Values 文件:

```
CNIMode: bbc-vpc-secondary-ip-auto-detect
Region: bj
VPCID: vpc-tvbxj4isix0h
ClusterID: baidu-k8s
ENISubnetList:
  - sbn-z03uhz0138xh
SecurityGroupList:
  - g-g2kkx6mh6a1t
PodSubnetList:
  - sbn-z03uhz0138xh
BCCEndpoint: bcc.bj.baidubce.com
BBCEndpoint: bbc.bj.baidubce.com
CCECNIImage: registry.baidubce.com/cce-plugin-pro/cce-cni:v1.3.1

ServiceCIDR: 192.168.0.0/16

AccessKeyID: ak # 用户 ak (用户自建集群需指定)
SecretAccessKey: sk # 用户 sk (用户自建集群需指定)

EnableVPCRoute: false
EnableStaticRoute: false
```


在填充完 values.yaml 文件后执行渲染出部署 yaml 进行检查:
```
make charts VALUES=build/yamls/cce-cni-driver/values.yaml
```
或者渲染后后直接提交至 K8s 集群:
```
make charts VALUES=build/yamls/cce-cni-driver/values.yaml | kubectl apply -f -
```

## 风险提示

- 在使用前请仔细阅读文档，避免 CNI 组件与当前网络配置发生冲突；
- 使用基于弹性网卡的容器网络方案，在节点删除后，VPC 内可能会残留弹性网卡，用户可以在删除机器时关联删除弹性网卡；