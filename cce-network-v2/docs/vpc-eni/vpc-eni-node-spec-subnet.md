# VPC-ENI Node 指定子网分配 IP
## 1. 概述
VPC-ENI Node 指定子网分配 IP是给每个节点指定独立的 eni 使用不同于集群子网的配置方法。

###  1.1 背景
VPC-ENI 模式容器网络，容器默认使用创建集群时通过 `eni-subnet-ids` 配置的子网列表，多个节点在子网列表中选择最少使用的子网创建 eni 并分配 IP 地址。为了满足不同业务场景，本文描述一种可以给每个节点指定 eni 使用不同于集群子网的配置方法。
### 1.2 适用场景
在使用 cmdb 等资源管理系统时，通常以业务线、项目组、应用等维度进行资源隔离，不同业务线、项目组、应用需要使用不同机器，且要求这个机器上的 Pod 使用该业务线专属的子网IP。在这种以业务为维度的机器资源管理模式下，为节点指定子网 IP 地址，可以满足不同业务线、项目组、应用需要使用不同机器的需求。

## 2. 设计方案
CCE 上通过在创建 Pod 前为 Node 添加 `network.cce.baidubce.com/node-eni-subnet-ids` Annotation 实现给 Node 指定子网分配 IP。例如：
```bash
kubectl annotate node cce-node-01 network.cce.baidubce.comnode-eni-subnet-ids="sbn-xxxxxx1,sbn-xxxxxx2"
```

为了兼容已有 BCC/EBC 集群使用集群子网的默认逻辑，在用户没有设置指定节点子网时，依然能够快速就绪工作，`cce-network-v2-config` 新增 `enable-node-annotation-sync` 配置项，默认为 false。如果 `enable-node-eni-subnet` 为 true，则开启 Node 到 nrs 对象的 annotation 同步逻辑，当用户为 Node 添加 `network.cce.baidubce.com/node-eni-subnet-ids` Annotation 时，会同步到 nrs 对象中。

由于节点在创建时，先创建 Node 对象，然后再由节点组控制器将 annotation 同步到 Node 对象中。因此，当 Node 在集群中创建时，可能存在短时间内 annotation 的未同步状态，此时需要等待节点组控制器将 annotation 同步到 Node 对象中。为此新增了 `node.cce.baidubce.com/resource-synced` 状态，当 Node Annotation中存在 `node.cce.baidubce.com/annotation-synced: "true"` 注解时，才开始读取 `network.cce.baidubce.com/node-eni-subnet-ids` Annotation配置。

如果设置了 `enable-node-annotation-sync` 配置项，在 Node 上没有 `node.cce.baidubce.com/annotation-synced: "true"` 注解时，容器网络持续等待，直到有 `node.cce.baidubce.com/annotation-synced: "true"` 注解后，才开始为节点分配 ENI。

开启 `enable-node-annotation-sync` 配置项后，如果用户修改了 Node 的 annotation 和 label 配置，则配置也会同步到 nrs 对象中。这需要 NodeDiscovery 控制器支持同步 Node 的 annotation 和 label的逻辑。


### 2.1 BCC/EBC 指定子网分配 IP
BCC/EBC 机型支持挂载弹性网卡 ENI，默认情况下，cce-network-operator 会使用用户创建集群时通过 `cce-network-v2-config` 配置的 `eni-subnet-ids` 子网列表，选择最少使用的子网创建 eni 并分配 IP 地址。在BCC/EBC 指定子网分配 IP场景中，用户需要为 Node 添加 `network.cce.baidubce.com/node-eni-subnet-ids` Annotation，指定节点的 eni 使用子网列表中的子网。

由于每个 ENI 在创建时指定了子网，因此用户需要保证子网列表中子网数量大于等于预期创建的 Pod 的总数。
ENI 创建后，不支持删除，因此需要保证子网列表中子网数量足够。请勿使用  `network.cce.baidubce.com/node-eni-subnet-ids` Annotation 删除之前已经指定的子网，因为 ENI 已经在节点上挂载成功，如果通过 annotation 把候选子网从列表中删除，则 BCC/EBC 实例将不再尝试为使用该子网的 ENI 分配辅助 IP。

### 2.2 BBC 指定子网分配 IP
BBC 机型不支持挂载弹性网卡 ENI，默认情况下，cce-network-operator 会使用用户创建集群时通过BBC主网卡所在的子网为容器分配 IP 地址。在 BBC 指定子网分配 IP 场景中，用户需要为 Node 添加主网卡子网之外的子网，这时就需要使用跨子网分配 IP 的特性。

当用户通过 `network.cce.baidubce.com/node-eni-subnet-ids` Annotation 指定多个子网时，cce-network-operator 会自动为 BBC 开启跨子网分配 IP 的特性。特性启用后，BBC 节点将不再使用主网卡辅助 IP 模式，而是使用跨子网分配 IP 模式。在 BBC IP 地址池容量不足时，会从候选子网列表中选择可用 IP 数量最多的子网，为容器分配 IP。

当`network.cce.baidubce.com/node-eni-subnet-ids`指定的候选子网列表发生删减时。如果被删减的子网已经分配了 IP，则不会立刻删除IP，而是直到地址池出现空闲，才会优先删除不在候选子网列表中的 IP。这是由于 IP 地址可能已经分配给了其他容器，因此需要等待 IP 地址空闲且被标记地址池空闲后才会删除。

## 3. 使用限制
* ENI 一旦创建，子网不能更换。有修改子网需求时，需要移除节点后重新创建。
* ENI 仅支持使用与实例相同的 VPC，同可用区的子网。
* 子网一旦创建，不支持扩容或其他修改操作。
* BBC 机型不支持 ENI，主网卡有 39 个辅助 IP 配额。
* EBC/BCC 机型支持的 ENI 和辅助 IP 数量请咨询百度智能云客服。
* 使用 BBC 指定子网分配 IP 时，请联系百度智能云客服。