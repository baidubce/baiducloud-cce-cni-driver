# vpc-eni ENI辅助IP容器网络
vpc-eni 是CCE容器网络提供的标准容器网络模式之一，其特色能力如下：
1. 完全打通百度智能云VPC，Pod的IP地址是ENI的辅助IP，地址分配自VPC子网。
2. 支持固定IP和跨子网分配IP策略
3. 兼容BCC/BBC/EBC机型，同时兼容主网卡辅助 IP 和 ENI 辅助IP模式
4. 支持IPv6
5.  BCC 支持跨子网分配 IP
# 1. 关键数据结构
## 1.1 NetResourceSet
每个core/v1.Node都有一个对应的NetResourceSet对象，描述节点上已经绑定的ENI和ENI辅助IP与Pod之间的分配情况，并从core/v1.Node同步便签.
```
apiVersion: cce.baidubce.com/v2
kind: NetResourceSet
metadata:
  name: 192.168.0.6
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: 192.168.0.6
    uid: a52f0da5-7b67-4deb-b040-7dd86fc09244
spec:
  addresses:
  - ip: 192.168.0.6
    type: InternalIP
  eni:
    availability-zone: zoneF
    installSourceBasedRouting: true
    instance-type: bcc
    maxAllocateENI: 2
    maxIPsPerENI: 8
    preAllocateENI: 1
    routeTableOffset: 127
    security-groups:
    - g-vp8fvkpini25
    subnet-ids:
    - sbn-iu5gyuds6s8m
    - sbn-w03en95xs1i9
    useMode: Secondary
    vpc-id: vpc-x0pimxnr49rf
  instance-id: i-mKtvuTyD
  ipam:
    pool:
      192.168.4.8:
        resource: eni-qv67kynxd6kz
        subnetID: sbn-iu5gyuds6s8m
      192.168.4.9:
        resource: eni-qv67kynxd6kz
        subnetID: sbn-iu5gyuds6s8m
      192.168.4.211:
        resource: eni-qv67kynxd6kz
        subnetID: sbn-iu5gyuds6s8m
    pre-allocate: 1
status:
  enis:
    eni-qv67kynxd6kz:
      cceStatus: ReadyOnNode
      id: eni-qv67kynxd6kz
      vpcStatus: inuse
  ipam:
    operator-status: {}
    used:
      192.168.4.8:
        owner: default/deployment-example-7bc5fdbb6f-cj2jt
        resource: eni-qv67kynxd6kz
        subnetID: sbn-iu5gyuds6s8m
      192.168.4.211:
        owner: default/deployment-example-7bc5fdbb6f-fshr8
        resource: eni-qv67kynxd6kz
        subnetID: sbn-iu5gyuds6s8m
```

* `spec.eni`
  * `availability-zone` 节点创建ENI的可用区。子网有可用区属性，BCC实例只能绑定使用同可用区下某个子网作为主IP的ENI。
  * `installSourceBasedRouting` 是否安装ENI源路由，在使用ENI辅助IP时请务必选择此项。因为BVS有数据包接口检查的流表，使用错误接口向外发送的数据包会被过滤。
  * `maxAllocateENI` 当前节点最大申请的ENI数。该数值通常遵守ENI配额限制规则。VPC在特殊情况下可以打破这个规则。所以v2的容器网络在启动参数中支持手动指定最大ENI数。
  * `maxIPsPerENI` 每个ENI最大绑定的IP地址数
* `spec.ipam.pool` : 由cce-network-operator为每个NetResourceSet分配的用于Pod容器的IP地址池。
  * 地址池中的IP是无状态的，即地址池中的IP不能用于固定IP使用。
  * 地址池中可能包含IPv4和IPv6地址
  * 地址池中不包含ENI主网卡IP
  * 地址池中不包含跨子网IP。

## 1.2 Subnet
子网是IP地址和网络路由的基础，由客户通过VPC控制台创建，由CCE触发VPC和CCE子网数据之间的同步。
```
# kubectl get sbn sbn-psupdhtdc0nv -oyaml
apiVersion: cce.baidubce.com/v1
kind: Subnet
metadata:
  labels:
    cce.baidubce.com/vpc-id: vpc-z1jv7cf7i4wv
  name: sbn-psupdhtdc0nv
spec:
  availabilityZone: cn-gz-c
  cidr: 172.16.80.0/20
  id: sbn-psupdhtdc0nv
  name: 系统预定义子网C
  vpcId: vpc-z1jv7cf7i4wv
  exclusive: false
status:
  availableIPNum: 3945
  enable: true
```
子网对象描述了用户VPC下的子网资源，该对象由部署集群时所指定的ENI辅助IP子网列表触发创建，默认每30s与VPC同步一次。

* `name`: VPC子网ID
* `spec`
    * `availabilityZone`: 子网的可用区
    * `cidr`: 子网网段
    * `exclusive`: 是否专属子网。这个属性在使用psts固定IP时非常重要，它代表了子网是否由单个CCE集群独享，并启用手动IP地址分配策略。独占子网的创建方法是，首先有客户通过VPC控制台创建子网，其次由客户在CCE中创建psts对象，并声明固定IP策略，最后psts关联的Subnet就会自动在CCE被创建，并被声明为独占。
* `status`
    * `availableIPNum`: 子网中剩余可分配IP数
    * `enable`: 是否启用子网

## 1.3 ENI
ENI对象用于描述VPC内的ENI弹性网卡，通常ENI会绑定到一个BCC虚机，由CCE管理并将ENI辅助IP分配给Pod。ENI是有配额的，一台BCC最多可绑定8个ENI。CCE上的ENI不允许删除和解绑，只有在NetResourceSet删除后，`eni-syncer`才会自动清理ENI对象。所以在设计上，ENI对象一定属于某个NetResourceSet对象。
每个 ENI 都最少绑定了一个安全组，用于设置容器网络的基本安全策略。修改安全组前，请确认安全组不会与容器网络流冲突，否则可能会导致容器的网络数据包被安全组规则拦截。

> 在BBC/EBC中，主网卡默认也是ENI

```
# kubectl get eni eni-m90jdaxu30f1 -oyaml
apiVersion: cce.baidubce.com/v2
kind: ENI
metadata:
  finalizers:
  - eni-syncer
  labels:
    cce.baidubce.com/instanceid: i-xMfUB4lZ
    cce.baidubce.com/node: 172.16.80.31
    cce.baidubce.com/vpc-id: vpc-z1jv7cf7i4wv
  name: eni-m90jdaxu30f1
  ownerReferences:
  - apiVersion: cce.baidubce.com/v2
    kind: NetResourceSet
    name: 172.16.80.31
    uid: 4dd0779d-921a-4b8e-b068-7c3db46cd7c8
spec:
  description: auto created by cce-cni, do not modify
  id: eni-m90jdaxu30f1
  installSourceBasedRouting: true
  instanceID: i-xMfUB4lZ
  macAddress: fa:28:00:15:6a:34
  name: cce-wddf4arj/i-xMfUB4lZ/172.16.80.31/c9ca01
  nodeName: 172.16.80.31
  privateIPSet:
  - primary: true
    privateIPAddress: 172.16.80.34
    subnetID: sbn-psupdhtdc0nv
  - privateIPAddress: 172.16.80.37
    subnetID: sbn-psupdhtdc0nv
  routeTableOffset: 126
  subnetID: sbn-psupdhtdc0nv
  type: bcc
  useMode: Secondary
  vpcID: vpc-z1jv7cf7i4wv
  vpcVersion: 6
  zoneName: cn-gz-c
status:
  CCEStatus: ReadyOnNode
  CCEStatusChangeLog:
  - CCEStatus: ReadyOnNode
    code: ok
    time: "2023-05-10T12:17:36Z"
  VPCStatus: inuse
  VPCStatusChangeLog:
  - VPCStatus: attaching
    code: ok
    time: "2023-05-10T12:16:32Z"
  - VPCStatus: inuse
    code: ok
    time: "2023-05-10T12:16:58Z"
  gatewayIPv4: 172.16.80.1
  index: 5
  interfaceIndex: 5
  interfaceName: cce-eni-0
  vpcVersion: 6
```

* `name`： ENI 的ID
* `spec`:
  * `instanceID`: ENI绑定的实例ID，它是BCC/BBC的短ID。
  * `macAddress`: ENI的mac地址，在整个ENI的生命周期中mac地址保持不变。
  * `name`: ENI的名字，CCE创建ENI的命名规则是`{clusterID}/{instanceID}/{nodeName}/{随机数}`。
  * `privateIPSet`: ENI关联的IP列表，每个ENI都有一个主IP，并有多个辅助IP，总体遵守[VPC中对ENI的配额规则](https://cloud.baidu.com/doc/VPC/s/9jwvytur6)。与ENI的某个内部IP有绑定关系的EIP也会在这里同步。
  * `IPv6PrivateIPSet`:ENI关联的IPv6地址列表。
  * `subnetID`: ENI 主IP所在的子网。
  * `type`: ENI的类型，包含`bcc`/`bbc`/`ebc`三个可选项。
  * `zoneName`: ENI的可用区。
  * `useMode`: ENI的使用模式，支持`Primary`独占ENI；`Secondry`ENI辅助IP两种模式；`PrimaryInterfaceWithSecondaryIP`主网卡和辅助IP模式。
  * `routeTableOffset`: 辅助IP模式下ENI使用的独立路由表偏移量。具体路由表ID的计算方法是 routeTableOffset + eniIndex。
  * `installSourceBasedRouting`: 是否为辅助IP安装策略路由规则。该属性用于控制在辅助IP模式下使用cptp插件时的策略路由的设置。
    * ENI 辅助 IP 模式支持安装策略路由规则
    * 主网卡辅助 IP 模式由于只有一个网卡，不支持策略路由。
* `status`
  * `CCEStatus`: ENI在单机上的状态。`Pending`: 单机还未发现ENI;`ReadyOnNode`: ENI已经在节点就绪; `UseInPod`: 独占ENI已经分配给Pod。
  * `CCEStatusChangeLog`: 状态变更历史，最多保存10条。
  * `VPCStatus`: ENI 在 VPC 的状态，`available`: 创建成功可以绑定到节点;`inuse`: 已绑定到节点。
  * `VPCStatusChangeLog`: 状态变更历史，最多保存10条。
  * `gatewayIPv4`: ENI 的路由网关地址。
  * `interfaceIndex`: ENI 的接口索引，指的是接口在操作系统上的序号。
  * `index`: ENI 在单机上的排序序号，从0-253.
  * `interfaceName`: ENI 在单机上的接口名，命名规则是 `cce-eni-{interfaceIndex}`。主网卡辅助 IP 模式中，接口使用操作系统的默认命名规则。

### 1.3.1 vpcVersion 同步机制
ENI 资源默认20s与VPC做一次同步，虽然同步周期较长，但`vpcVersion`机制为了保证ENI数据的时效。`vpcVersion`是基于事件的同步，每当集群中有辅助IP申请或释放事件，会立刻触发ENI与VPC的同步。

# 2. 使用限制
VPC-ENI 模式使用限制如下：
* 容器网络的 ENI 不支持用户手动管理，仅支持自动创建和随云主机销毁而自动释放。
* 已有 ENI 的云主机无法加入 VPC-ENI 容器网络。因为已有 eni 的云主机所绑定的子网可能不在容器网络管理的子网范围内，影响正常的 IP 地址分配。

## 2.1 可用区限制
* VPC-ENI 模式所有的节点都必须在同一个 VPC 内
* VPC-ENI 模式仅允许添加有可用容器子网的可用区的云主机
  * 每个子网都有可用区属性，例如 zoneA。
  * 每个云主机也都有可用区属性，例如BCC 云主机属于 zoneA。
* ENI 和云主机实例必须属于同一个可用区，不可跨可用区使用容器子网。例如在 zoneB 的云主机使用 zoneA 的子网。

## 2.2 VPC子网 配额限制
* 每个VPC最多可创建10个子网。
* VPC 一旦创建，地址空间则不能修改，能容纳的IP地址数量亦不能修改。
* **子网一旦创建，可用区和地址空间则不能修改，能容纳的IP地址数量亦不能修改。**

## 2.2 弹性网卡配额限制
* **ENI 弹性网卡一旦创建，只能选择加入某个VPC下的一个子网，不能指定IP，也不支持修改子网移出。**
* 每个VPC弹性网卡最大数量500个。
* 单网卡上IP数量最少1个，最多40个。
* 云主机可挂载弹性网卡数量=min（主机核数，8）。
* 云主机绑定的网卡上可配置IP数量。

| 内存 | 	IP数量|
| -- | -- |
| 1G	| 2 |
|（1-8]G |	8 |
| (8-32]G |	16 |
| (32-64]G |	30 |
| 大于64G	| 40 |