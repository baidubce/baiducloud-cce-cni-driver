# 主网卡辅助 IP 模式
为了达到简单且一致的产品体验，主网卡辅助 IP 模式在不支持 ENI 的机型上，例如 BBC 和部分 EBC 机型，可以利用在主网卡上配置辅助 IP 的方式，实现每个 Pod 都有一个原生于 VPC 的 IP 地址身份的能力。

## 优势
主网卡辅助 IP 模式，在主网卡上配置辅助 IP，然后通过 ENI 的 SecondaryIP 特性，将辅助 IP 绑定到 ENI。辅助 IP 与网卡的主 IP 使用相同的子网，因此可以保证辅助 IP 与主 IP 在 VPC 中的同等地位。
与 ENI 辅助 IP 模式相比，主网卡辅助 IP 模式下舍弃了策略路由能力，因此这种模式的数据路径更为简洁易懂。且在主网卡辅助 IP 模式下，可以支持所有机型，拥有更强的兼容性。

## 使用限制
* **请保证实例所在子网 IP 地址空间充足**，至少预留 n * c 个辅助 IP 地址容量的子网空间。
    * n 是所有机器的数量。
    * c 是每个机器最大可分配辅助 IP 数量。
    * 子网容量应在购买机器前提前规划，一旦机器加入集群，无法再修改子网。
* BBC/EBC 每个实例最多支持 39 个辅助 IP 地址。当节点调度了超过 39 个 Pod 时，CNI 无法为 Pod 分配 IP 地址。
* 节点加入 CCE 集群前，请确保是纯净的节点，即节点上没有运行过任何容器。推荐将节点执行操作系统重装。
* 使用 VPC-ENI 模式，推荐内核版本为 5.7 以上。

# 实现原理
主网卡辅助 IP 模式在技术实现上为 ENI 增加了一种新的使用模式 `PrimaryWithSecondaryIP` 。BBC 机型默认使用这种模式，而 EBC 机型则根据百度云 EBC 产品的定义，自动决策使用 `PrimaryWithSecondaryIP` 还是 `Secondary`。
## BBC 机型案例
BBC 机型默认使用 `PrimaryWithSecondaryIP` 模式，其网络集合对象实例如下：

```yaml
apiVersion: cce.baidubce.com/v2
kind: NetResourceSet
metadata:
  annotations:
    cce.baidubce.com/ip-resource-capacity-synced: "2024-02-21T02:26:28Z"
  generation: 3
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/instance-gpu: "false"
    beta.kubernetes.io/instance-type: BBC
    beta.kubernetes.io/os: linux
    cce.baidubce.com/baidu-cgpu-priority: "false"
    cce.baidubce.com/gpu-share-device-plugin: disable
    cce.baidubce.com/kubelet-dir: 2f7661722f6c69622f6b7562656c6574
    cluster-id: cce-z85m1xdk
    cluster-role: node
    failure-domain.beta.kubernetes.io/region: bj
  name: 10.0.19.7
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: 10.0.19.7
    uid: 45341763-2b46-472b-884e-695974a67eaf
  resourceVersion: "13952347"
  uid: d479844c-5519-4d08-a7bc-f850179d3311
spec:
  addresses:
  - ip: 10.0.19.7
    type: InternalIP
  eni:
    availability-zone: zoneD
    instance-type: bbc
    maxAllocateENI: 1
    maxIPsPerENI: 40
    preAllocateENI: 0
    useMode: Secondary
    vpc-id: vpc-w829y8wpni8i
  instance-id: i-1SvcOgBl
  ipam:
    pool:
      10.0.19.8:
        resource: eni-ih1seearb7y8
        subnetID: sbn-kx327kp355ni
      10.0.19.9:
        resource: eni-ih1seearb7y8
        subnetID: sbn-kx327kp355ni
      10.0.19.10:
        resource: eni-ih1seearb7y8
        subnetID: sbn-kx327kp355ni
      10.0.19.11:
        resource: eni-ih1seearb7y8
        subnetID: sbn-kx327kp355ni
status:
  enis:
    eni-ih1seearb7y8:
      cceStatus: ReadyOnNode
      id: eni-ih1seearb7y8
      subnetId: sbn-kx327kp355ni
      vpcStatus: inuse
  ipam:
    operator-status: {}
    used:
      10.0.19.10:
        owner: kube-system/nfd-worker-f244k
        resource: eni-ih1seearb7y8
        subnetID: sbn-kx327kp355ni
```

上面的实例中，以下字段域需要注意：
* `spec.eni.useMode` 字段为 `Secondary`，表示使用 ENI 辅助 IP 模式。
* `spec.eni.instance-type` 字段为 `bbc`，表示使用 BBC 机型。
* `spec.eni.maxIPsPerENI` 字段为 40，表示每个 ENI 的最大辅助 IP 数量
* `status.enis.eni-ih1seearb7y8` 代表一个 ENI 对象，其中 `id` 为 ENI ID，`subnetId` 为子网 ID。百度云每个主机的主网卡，也是一个抽象的 ENI 对象。

### BBC ENI 实例
BBC 实例的 ENI 对象示例如下：
```yaml
apiVersion: cce.baidubce.com/v2
kind: ENI
metadata:
  creationTimestamp: "2024-02-21T02:26:28Z"
  finalizers:
  - eni-syncer
  generation: 2
  labels:
    cce.baidubce.com/eni-type: bbc
    cce.baidubce.com/instanceid: i-1SvcOgBl
    cce.baidubce.com/node: 10.0.19.7
    cce.baidubce.com/vpc-id: vpc-w829y8wpni8i
  name: eni-ih1seearb7y8
  ownerReferences:
  - apiVersion: cce.baidubce.com/v2
    kind: NetResourceSet
    name: 10.0.19.7
    uid: d479844c-5519-4d08-a7bc-f850179d3311
  resourceVersion: "13952271"
  uid: 0d1631b9-68ff-42d0-a1b0-dcb9922fd55f
spec:
  id: eni-ih1seearb7y8
  instanceID: i-1SvcOgBl
  macAddress: fa:20:20:31:4e:53
  name: eth0
  nodeName: 10.0.19.7
  privateIPSet:
  - primary: true
    privateIPAddress: 10.0.19.7
    subnetID: sbn-kx327kp355ni
  - privateIPAddress: 10.0.19.10
    subnetID: sbn-kx327kp355ni
  - privateIPAddress: 10.0.19.11
    subnetID: sbn-kx327kp355ni
  - privateIPAddress: 10.0.19.8
    subnetID: sbn-kx327kp355ni
  - privateIPAddress: 10.0.19.9
    subnetID: sbn-kx327kp355ni
  subnetID: sbn-kx327kp355ni
  type: bbc
  useMode: PrimaryWithSecondaryIP
  vpcID: vpc-w829y8wpni8i
  vpcVersion: 1
  zoneName: cn-bj-d
status:
  CCEStatus: ReadyOnNode
  CCEStatusChangeLog:
  - CCEStatus: ReadyOnNode
    code: ok
    time: "2024-02-21T02:26:47Z"
  VPCStatus: inuse
  VPCStatusChangeLog:
  - VPCStatus: inuse
    code: ok
    time: "2024-02-21T02:26:28Z"
  interfaceIndex: 2
  interfaceName: eth0
  vpcVersion: 1
```

上面的实例中，以下字段域需要注意：
* `spec.useMode` 字段为 `PrimaryWithSecondaryIP`，表示使用主网卡辅助 IP 模式。
* `spec.type` 字段为 `bbc`，表示使用 BBC 机型。
* `spec.subnetID` 字段为 ENI 的子网 ID。ENI 的主 IP 一旦确定，子网 ID 不可变更。
* `spec.privateIPSet` 字段为 ENI 的辅助 IP 列表，其中 `primary: true` 表示该 IP 为主IP.
* `status.interfaceIndex` 字段为 ENI 的接口的网卡设备索引。
* `status.interfaceName` 字段为 ENI 的接口名称。
* `status.vpcVersion` 字段为 VPC 的版本号，用于与 VPC 做状态同步使用，默认每 20s 与 VPC 做一次状态同步。
* `status.VPCStatus` 字段为 VPC 的状态，`inuse` 表示 VPC 记录的网卡可用且在使用中。
* `status.CCEStatus` 字段为CCE的单机引擎记录的 ENI 状态，`ReadyOnNode` 表示 ENI 记录的网卡可用且在使用中。
