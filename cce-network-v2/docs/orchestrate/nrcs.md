# 节点网络配置集功能 NetResourceConfigSet
CCE 集群创建成功后，默认使用`kube-system`命名空间下 `cce-network-config` ConfiMap 配置集群网络。如果需要按节点/节点组配置网络，并覆盖默认网络配置，则需使用 NetResourceConfigSet 对象配置部分节点网络。

## 简介
NetResourceConfigSet 用于配置部分节点的网络，其配置信息将与 `kube-system` 命名空间下 `cce-network-config` ConfiMap 的配置信息合并。

## 示例
### 配置节点ENI 的子网和安全组
```yaml
apiVersion: cce.baidubce.com/v2alpha1
kind: NetResourceConfigSet
metadata:
  name: nrc-example
spec:
    # 多个 NetResourceConfigSet 对象可以配置相同的 labelSelector，但仅一个生效，priority 越大越优先
    priority: 0
    # 配置 labelSelector，指定节点的标签，不可为空
    selector:
        matchLabels:
            instanceGroup: "nodegroup-example"
    agent:
        eni-subnet-ids:
        - sbn-0001
        - sbn-0002
        eni-security-group-ids:
        - sg-0001
        burstable-mehrfach-eni: 1
```

上述配置完成了对符合 `instanceGroup: "nodegroup-example"` 标签的节点做了两个网络配置:
- 配置了 ENI 的子网为 `sbn-0001` 和 `sbn-0002`
- 配置了 ENI 的安全组为 `sg-0001`
- 其它未直接指明的配置项保持默认值

## 生效范围
NetResourceConfigSet 通过 labelSelector 关联节点，其 labelSelector 配置信息与 `v1.Node` 对象的标签相互关联，指明 NetResourceConfigSet 的生效范围。

> 通过给节点组打标签，将节点分组，然后给节点组打 labelSelector 标签，可以配置节点组的网络。

## 支持的参数列表
NetResourceConfigSet 支持配置以下网络参数：
| 配置项 | 描述 | 类型 | 默认值 |
| --- | --- | --- | --- |
| `enable-rdma` | 是否开启 RDMA | bool | `false` |
| `eni-use-mode` | ENI 模式 | string | `"Secondary"` 普通 ENI |
| `eni-subnet-ids` | ENI 子网列表 | string[] | `""` |
| `eni-security-group-ids` | ENI 安全组 | string[] | `""` 企业安全组 ID |
| `eni-enterprise-security-group-ids`| 节点 ENI 的企业安全组 | string[] | `""` |
| `burstable-mehrfach-eni` | 启用burstable ENI池，默认使用保持最少 1 个 ENI 空闲 | int | `1` |
| `ext-cni-plugins` | 额外开启的 CNI 插件列表 | string[] | `"endpoint-probe"` |
| `ippool-pre-allocate-eni` | 预分配 ENI 的数量 | int | `1` |
| `ippool-min-allocate` |  仅在`burstable-mehrfach-eni`为 0 时有效，ENI 每次申请最小 IP 数 | int | `0` |
| `ippool-pre-allocate` |  仅在`burstable-mehrfach-eni`为 0 时有效，IP 池中最小可用 IP 数 | int | `0` |
| `ippool-max-above-watermark` |  仅在`burstable-mehrfach-eni`为 0 时有效，IP 池中最大空闲 IP 数 | int | `0` |

## 使用限制
- CCE 容器网络插件版本为 `v2.12.0` 或以上。
- 配置的节点必须符合 `selector` 的 labelSelector，否则将无法生效。
- 配置的子网、安全组必须存在，否则将无法生效。
- 配置的子网、安全组必须与节点所在可用区匹配，否则将无法生效。
- 仅新建 ENI 时配置才会生效，如果已经创建的 ENI 配置了子网、安全组，将不会生效。
- 修改配置后默认 10h 才会对已有节点生效，配置了 nrcs 后需要重启已有的节点cce-network-agent 才会立即生效。
- 如果在 BBC 上使用指定子网功能，请确认已经开启了跨子网分配 IP 白名单。
- 每个节点最多有一个生效的 `NetResourceConfigSet`。

## 核心原理
### agent 启动逻辑
cce-network-agent 启动时，默认会从 flag 中读取配置参数，而指定节点配置网络的目标就是要覆盖默认参数。所以在设计 nrsc 时，需要注意 nrcs 和 cce-network-agent 组件生命周期的关系。
1. cce-network-agent 需要先获取 flag 才能解析运行环境，开始启动。
2. 在启动后，需要等 nrcs 的 informer 同步完成后，才允许工作。
3. 在创建 NetResourceSet 对象前，需要筛选出符合 labelSelector 的 nrcs（使用 Node 的 label 作为筛选元数据） ，并根据 `spec.priority` 做排序，取数字最大的配置，应用于节点。
4. nrcs 对象暂时不需要记录状态，也不支持修改后立刻生效。

### 配置生效逻辑
#### 新建节点逻辑
对于所有配置项，在新增节点时，如果有对应匹配的 nrcs 对象，则会立即生效。以下配置项仅在新增节点时有效：
- `eni-use-mode`
- `use-eni-primary-address`
- `route-table-offset`

#### 已有节点配置变更
对于可运行时变更配置，用户在配置了 nrcs 对象，重启 cce-network-agent 后，会触发 cce-network-agent 重新加载配置，并更新节点状态。
为了准守配置一致性，CCE 当前仅支持以下配置项可运行时变更：
- `enable-rdma`
- `eni-subnet-ids` 
- `eni-security-group-ids`
- `eni-enterprise-security-group-ids`
- `burstable-mehrfach-eni`
- `ext-cni-plugins`
- `ippool-pre-allocate-eni`
- `ippool-min-allocate`
- `ippool-pre-allocate`
- `ippool-max-above-watermark`

#### 已有节点修改子网逻辑
nrcs 支持修改已有节点的子网 和安全组，但仅在新建 ENI时才生效。
当操作 nrcs 删除子网时，如果节点上已使用子网创建 ENI，则 nrcs 中不会实际删除子网。
当操作 nrcs 新增子网时，节点上新建的 ENI 时默认将从多个子网中选择可用 IP 最多的子网创建 ENI。

例如节点 A 当前已经配置了 sbn-1, sbn-2 两个子网。当 nrcs 配置删除sbn-2时：
* 如果 sbn-2 已经在节点 A 上创建了 ENI， 则无法删除 sbn-2。且后续新建的 ENI 可能再使用 sbn-2。
* 如果 sbn-2 未在节点 A 上创建 ENI，则 nrcs 会删除 sbn-2。后续新建的 ENI 都不会再使用 sbn-2。

> 如果需要完全删除某个已被使用的子网，需要先删除节点，再删除 nrcs 中的配置。
### RDMA 支持
cce-network-v2 RDMA 发现的节点支持 RDMA 时，默认会给 Pod 分配 RDMA 网卡。通过 nrcs 可以配置是否开启 RDMA。