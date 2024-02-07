## 概览

VPC-ENI容器网络模式支持指定子网为Pod分配IP，用户可以使用这个能力为不同的业务应用Pod规划分配不同子网的IP地址。本文档介绍了一种对`psts`的扩展，即集群级子网拓扑分布策略（`cpsts`）。

> 说明：使用该功能需要联系百度智能云客服开启白名单。

## 需求场景
`psts` 提供了 3 种分配 IP 的策略，可以用于解决动态 IP 地址分配、手动 IP 地址分配和固定 IP 的场景问题。但一个`psts`仅在一个 namespace 中生效，如果需要为不同业务应用有相同的 IP 地址分配策略，则需要创建多个`psts`，重用起来比较复杂。
`cpsts` 提供了集群级子网拓扑分布策略，只需要创建一个`cpsts`对象，可以在多个 namespace 下生效，解决多个 namespace 的 IP 地址分配策略问题。

## 方案概述

如下CRD数据结构所示，CCE提供了自定义CRD来指定Pod子网拓扑分布策略，用于在CCE集群中实施制定子网分配IP功能。

### 关键数据结构介绍
 #### 集群Pod子网拓扑分布 ClusterPodSubnetTopologySpread
 集群子网拓扑分布对象(`cpsts`)在工作时会根据`namespaceSelector`在符合条件的 Namespace 下创建Pod子网拓扑分布(`psts`)对象。并依赖`psts` 影响 Pod 的 IP 地址分配。它的数据结构定义如下：
 ```
apiVersion: cce.baidubce.com/v2alpha1
kind: ClusterPodSubnetTopologySpread
metadata:
  name: cpsts-1
spec:
  name: cpsts-1
  # 选择 cpsts 对象生效的 namespace
  namespaceSelector:
    matchLabels:
      zrq: test
  # 选择在每个 namespace 下 cpsts 对象生效的 Pod
  selector:
    matchLabels:
      app: redis
  subnets:
    - sbn-6mrkdcsyzpaw:
      # 非必须，固定 IP 的范围，不填表示使用子网默认 IP 范围
      - family: 4
      range:
      - start: 10.0.0.2
        end: 10.0.0.254
  strategy:
    releaseStrategy: TTL
    ttl: 168h0m0s
    type: Elastic
    # 开启 IP 重用
    enableReuseIPAddress: true
  whenUnsatisfiable: DoNotSchedule
 ```
 该对象的核心字段如下：
 

|  域| 数据类型| 必须| 默认值| 描述
| --- | --- | --- | --- | --- |
|  `name`| `string` | | |拓扑分布对象名。|
| `priority` | `int32`| 否| 0| 多个子网拓扑约束之间的优先级，数字越大，优先级越高。|
| `namespaceSelector` | `object` | 否 | | 使用该条件对 namespace 做标签匹配，符合条件的 namespace 中将会创建 `psts` 子对象。<br/>如果不填写这个选项，则 cpsts 默认在所有的 namespace 下都创建一个名为 `{namespace}-{cpstsName}` 的`psts`对象。 |
| `selector` | `object` | 否 | | 使用该条件对Pod做标签匹配，符合条件的Pod将在分配IP地址时使用该规则。如果`selector`为空，则同namespace下的所有Pod都匹配该规则。|
| `subnets` | `object` | 是 | | 策略要使用的子网。CCE会从这些子网中为Pod分配IP地址。| 如果使用了多个`subnet`，多个`subnet`类型必须一样，不允许混用专属子网和普通子网|
| `subnets.[].family` | `string` | 是 | | IP 地址协议族。取值 `"4"` 或 `"6"`|
| `subnets.[].range` | `array` | 是 | | IP 地址范围 |
| `subnets.[].range[].start` | `string` | 是 | | 起始 IP 地址 |
| `subnets.[].range[].end` | `string` | 是 | | 结束 IP 地址 |
| `strategy` | `object` | 是 | | IP 地址使用和回收策略 |
| `strategy.type`| `string` | 是 | Elastic | `Elastic`: 动态分配 IP 地址，可以使用任意工作负载； `Fixed`：永久固定 IP 地址，仅搭配 sts 工作负载使用；`PrimaryENI`：独占 ENI 专用 IP 地址 |
| `strategy.releaseStrategy` |`string` | 是 |  TTL | IP 地址释放策略。 `TTL`： Pod 被删除后，IP 地址随时间过期。其中在动态 IP 分配模式下，Pod 删除后，IP 立刻回收。 开启 `enableReuseIPAddress` 时，默认 7 天回收。<br/>  `Never`： 仅搭配 `strategy.type: Fixed` 使用，代表永不回收。 |
| `strategy.enableReuseIPAddress` |`bool` | 否 |   false | 是否在 `strategy.type: Elastic` 的场景下开启 IP 重用。如开启 IP 重用，则在 IP 地址过期之前，如果有同名的 Pod 重复创建，则尽力去复用 IP，以达到类似固定 IP 的效果。 |
| `strategy.ttl` |`string` | 否 |   168h0m0s | 在开启 IP 地址重用时，Pod 删除后，保留 IP 的时间。默认值是 7 天(168h0m0s) |


### 使用限制
1. 该功能需使用VPC的ENI跨子网分配IP功能，请提交工单申请使用ENI跨子网分配IP功能。
2. `kube-system` 命名空间的Pod无法使用指定子网分配IP功能。
3. 使用`ipRange`能力时，注意IP范围内不要包含等特殊IP地址（如IPv4网络地址、网关地址、广播地址、多播地址），否则可能产生无法正常分配IP。
4. 指定子网的Pod只能调度到子网所在可用区的节点，请确认可用区有Ready状态的节点。
5. 固定IP和IP复用场景下只能使用专属子网（指定的子网只能供单个CCE集群使用），专属子网不支持变更为普通子网、不支持从集群内删除，详情见专属子网说明。
6. 该功能仅适用于容器网络 v2 版本的集群。

> **专属子网**：
 当用户需要为Pod分配指定子网下的某几个IP时，IP所属的子网将自动被标记为手动分配IP模式。手动分配IP模式下的子网有以下特征：
1. 专属子网应为当前CCE集群专属，CCE会自动为子网增加专属标签，其它的CCE集群拒绝使用该子网。（用户可以操作百度云其它产品使用该子网）
2. 对专属子网仅支持手动分配IP，不会自动分配IP。这需要用户自己管理IP地址规划
3. 专属子网下IP和Pod的关系可以有优先分配和固定绑定两种。固定绑定策略使用Pod名作为绑定标识，同一个Pod名的IP地址相同
4. 集群Pod的默认子网不能是专属子网，否则可能导致集群中的其它Pod无法正常分配IP
5. 专属子网不支持变更为普通子网、不支持从集群内删除
 
## 配置步骤

### 环境准备

#### 创建私有子网
在[百度智能云VPC控制台->子网选项卡](https://console.bce.baidu.com/network/#/vpc/subnet/list)，为自己的VPC新建一个子网，并保存子网ID （以`sbn-xxx`格式命名的是子网ID）。注意创建子网时应选择与CCE集群节点相关的可用区，否则可能导致调度失败。

> 说明：
如需使用ENI跨子网分配IP功能，请提交[工单](https://ticket.bce.baidu.com/?_=1668763831200#/ticket/create)申请。

![image](https://ku.baidu-int.com/wiki/attach/image/api/imageDownloadAddress?attachId=0541648c88be457ca0973cf62c9fa85d&docGuid=NLxLplMpVg3AEA&sign=eyJhbGciOiJkaXIiLCJlbmMiOiJBMjU2R0NNIiwiYXBwSWQiOjEsInVpZCI6ImR3TmJQaUJoQTYiLCJkb2NJZCI6Ik5MeExwbE1wVmczQUVBIn0..Z9Ow4eJl-nz2PxXN.gOvbPTuRCfa2RPoLOFjVeBQOPnlWtpVUvGixaABVxX0qCYTFoR6BnbQ1HncncXrOkibhbAO6sOfh3lXmC8MJndBIPpgzKiPaQf26rVjFttUzabNWWomO3dpyBjNMMNlw6Urh7LFOsCSqELNFq1OX1w2-ErfkXcHYPVAs810Q6HdPaHbMAx3DbvPA2RyAyT0RiHkwih5KU-cfuAoVkV-n_-KhsA.o5R6iUXeMB07EmBpTinKBg)

### 在CCE上操作指定子网分配IP

#### 场景1： 全集群多应用固定 IP
从与Pod匹配的子网拓扑约束策略关联的子网中动态为Pod分配IP。匹配了子网拓扑约束策略（`cpsts`）的所有Pod只从策略包含的子网分配IP地址。
适用场景：
* 以子网维度对流量做统计
* 以子网维度做安全策略，如ACL规则控制
* 通过NAT网关为特定子网开启互联网访问
##### 1. 创建cpsts
 ```
apiVersion: cce.baidubce.com/v2alpha1
kind: ClusterPodSubnetTopologySpread
metadata:
  name: cpsts-1
spec:
  name: cpsts-1
  # 选择 cpsts 对象生效的 namespace
  namespaceSelector:
    matchLabels:
      zrq: test
  # 选择在每个 namespace 下 cpsts 对象生效的 Pod
  selector:
    matchLabels:
      app: fixedIPApp
  subnets:
    - sbn-6mrkdcsyzpaw:
      # 非必须，固定 IP 的范围，不填表示使用子网默认 IP 范围
      - family: 4
      range:
      - start: 10.0.0.2
        end: 10.0.0.254
  strategy:
    releaseStrategy: TTL
    ttl: 168h0m0s
    type: Elastic
    # 开启 IP 重用
    enableReuseIPAddress: true
  whenUnsatisfiable: DoNotSchedule
 ```
##### 2. 创建工作负载
```
apiVersion: apps/v1
# 必须使用有状态工作负载
kind: StatefulSet
metadata:
  name: foo
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: fixedIPApp
  serviceName: foo
  template:
    metadata:
      labels:
        workloadType: sts
        fixedIP: "true"
        app: fixedIPApp
    spec:
      containers:
          - image: nginx
            name: nginx
```
##### 3. 验证IP分配结果
```
# kubectl get pod {podName} -oyaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    cce.baidubce.com/PodSubnetTopologySpread: example-subnet-topology
  labels:
    app: foo
  name: foo-0
  namespace: default
spec:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: topology.kubernetes.io/zone
            operator: In
            values:
            - zoneF
  containers:
  - image: nginx
    imagePullPolicy: IfNotPresent
    name: nginx
    resources:
      limits:
        cce.baidubce.com/ip: "1"
      requests:
        cce.baidubce.com/ip: "1"
```

## 相关产品

* [私有网络VPC](https://cloud.baidu.com/product/vpc.html)