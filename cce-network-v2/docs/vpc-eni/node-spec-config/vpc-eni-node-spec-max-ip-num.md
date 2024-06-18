# VPC-ENI Node 指定 ENI 最大辅助 IP 数量
## 1. 概述
本文介绍了通过创建节点/节点组时通过 annotation 自定义节点上 ENI 最大辅助 IP 数量。通过用户增加自定义 annotation 的方式，灵活的控制每个节点的容器密度。为网络资源规划，资源高效利用提供高级自定义能力。

###  1.1 背景
### 1.1.1 ENI 最大辅助 IP 数量
VPC-ENI Node支持的最大 ENI 数量遵守[BCC节点规格定义](https://cloud.baidu.com/doc/BCC/s/wjwvynogv)，而EBC/BCC 机型每个 ENI 最大 IP 数量默认的算法如下：
| 内存 | ENI 最大 IP 数量 |
| --- | --- |
| (0-2)G | 2 |
| [2,8)G | 8 |
| [8,32)G| 16|
| [32,64)G | 30 |
| >= 64 G| 40 |

BBC 机型默认不支持 ENI，主网卡允许的最大辅助 IP 数量为 40 个。

### 1.1.2 单机最大 ENI 数量
EBC/BCC 机型最大 ENI 数量遵守[BCC节点规格定义](https://cloud.baidu.com/doc/BCC/s/wjwvynogv)，而BBC 机型最大 ENI 数量为 1。

### 1.2 适用场景
* 节点上需要创建更少的容器，方便保证单个容器的服务质量。
* 节点上需要创建更多的容器，甚至需要超出上述 BCC/EBC/BBC 机型的 IP 规模约束，方便保证集群的资源利用率。

## 2. 设计方案
CCE 上通过为 Node 添加 `network.cce.baidubce.com/node-eni-max-ips-num` Annotation 实现给 Node 自定义 ENI 最大辅助 IP 数量。例如：
```bash
kubectl annotate node cce-node-01 network.cce.baidubce.com/node-eni-max-ips-num="40"
```

当用户未通过 annotation 指定ENI最大IP数量时，cce-network-operator 会使用上述算法为 Node 自动分配 ENI IP 数量，并尝试将 IP 分配给容器。当用户指定了 ENI 最大 IP 数量时，cce-network-operator 会使用用户指定的数量为 Node 创建 ENI。

CCE 上通过为 Node 添加 `network.cce.baidubce.com/node-max-eni-num` Annotation 实现给 Node 自定义最大 ENI数量。例如：
```bash
kubectl annotate node cce-node-01 network.cce.baidubce.com/node-max-eni-num="1"
```

## 3. 使用限制
* `network.cce.baidubce.com/node-eni-max-ips-num` 应大于子网可用 IP 容量，请在设置自定义 ENI 最大 IP 数量时，做好集群网络规划，注意子网可用 IP 容量。
* `network.cce.baidubce.com/node-eni-max-ips-num` 指定的辅助 IP 数量不可超过实际 ENI 最大 IP 数量的规格配额。如有提升配额需求，请联系百度智能云客服支持。
* BCC/EBC/BBC 每个计算实例可支持 e * （n - 1） 个辅助 IP。
    * e 表示节点最大支持的 ENI 数量。
    * n 表示每个 ENI 最大支持的 IP 数量。，默认每个 ENI 实例最大可支持 40 个辅助 IP。
* `network.cce.baidubce.com/node-max-eni-num` 实例支持的最大 ENI 数量不允许超过实例的规格限制，否则会导致实例无法正常创建 ENI。
* `network.cce.baidubce.com/node-max-eni-num` 不可小于实例已经绑定的 ENI 数量。