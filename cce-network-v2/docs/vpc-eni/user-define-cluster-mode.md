# 自定义规划集群模式
自定义规划集群模式，即用户在创建集群时，需要指定子网和 IP 地址范围。系统会根据用户指定的子网和地址范围，推测出集群的最大节点规格和最大 Pod 数量。帮助用户对集群进行规划，避免用户在创建集群后，再配置 IP 地址范围。
在这种模式下，用户需要提前规划好子网和 IP 地址范围，并确保 IP 地址范围足够。CCE 会使用用户已经规划好的子网和 IP 地址范围，创建集群，并为集群中的 Pod 分配用户所期望的 IP 地址。
## 1. 自定义规划集群模式的目标
* 用户可以精细化控制集群的资源使用，满足不同场景下用户的需求。
* 增加资源规划能力：集群最大可以支持 5000 节点，集群最多支持 15000 个 Pod。在不能满足这个最大集群容量时，给用户足够的提示，避免用户在创建集群后，再配置 IP 地址范围。
* 增加对故障隔离能力，提升稳定性: 当子网不足以创建新的ENI，且为 ENI 分配所有 IP 时，可以及时的把节点标记为非就绪状态。避免新的 Pod 调度到该节点上，无法分配 IP 地址，导致用户的变更无法生效。
* 增加故障发现的精准度：当子网资源不足时，可以及时发现故障，并把节点标记为不可用状态。依赖这个状态，上层能实现报警，及时并准确发现子网资源不足的情况，提醒用户尽快扩容。

## 2. 系统现状（无规划的集群）
CCE VPC-ENI 现状是无规划的集群模式，对普通用户来说所存在的问题主要如下：
1. 用户创建集群时可以选择容器子网，但无法知道最大节点数量和最大 Pod 数量。对百度智能云配额不熟悉的用户，无法根据实际需求，规划好集群。
  1.1 很多用户在不知情的情况下，创建了仅能容纳 32 个节点的集群，且创建的集群节点数无法扩容。
  1.2 非资深专家用户无法创建出 5000 节点，150000 Pod 的集群。
2. 故障无法及时发现：子网资源耗尽本是符合预期的，但因子网 IP 地址不足无法创建新 Pod时，无法及时报警，导致用户无法及时添加新的子网到 CCE 集群配置。
    2.1 由于子网是用户控制的资源，创建子网就是为了把子网的 IP 地址分配给业务或者 lb 等网元。所以大部分场景下子网 IP 耗尽是符合预期的。
3. 故障无法隔离：Pod 在节点上无法分配 IP 地址时，还会继续向节点调度 Pod，使更多的 Pod 无法正常启动。
4. 故障不可无损恢复：节点因子网可用 IP 不足，无法继续分配 IP 地址。在如下经常被触发的条件下，会陷入无法恢复的状态，仅能通过更换新节点来解决问题：
  4.1 节点可绑定的 ENI 配额耗尽。例如单实例只能绑定 8 个 ENI，且已经绑定 8 个 ENI。
  4.2 VPC 限制一个 ENI 只能使用一个子网，且ENI不支持变更子网。
  4.3 子网不可扩容。
  4.4 用户无法通过添加新的子网来解决问题。

## 3. 自定义规划集群模式
自定义规划集群模式，即用户在创建集群时，需要指定容器子网。系统会根据用户指定的子网和地址范围，推测出最大节点容量和最大 Pod 数量。帮助用户对集群进行规划，避免用户在创建集群后，再因创建集群时指定的参数限制，无法扩容节点，或者业务变更因此受到阻塞。自定义规划集群模式主要通过以下几个关键点来实现：
* 资源规划推算：根据用户指定的子网，推测出多种机型下集群的最大节点规格和最大 Pod 数量。
  * 根据百度云最新的 BCC 机型，推测出多种不同规格下，最大节点数量和最大 Pod 数量。
  * 根据用户指定的子网，推测出预计会占用和浪费的总 IP 数量与占比。
* Burstable ENI 池： 以 ENI 为基本单位，重构容器网络 nrs IP 地址池。每个 ENI 的容量从共享子网中抢占IP 地址，一旦抢占成功，ENI 生存期内永不释放。
* 故障隔离能力：当节点需要创建新 ENI，但由于子网资源不足或者其他原因无法创建新 ENI 时，主动隔离故障节点。
* 故障发现：当发现故障隔离节点时，暴漏问题指标，并报告问题事件。

### 3.1 资源规划推算
用户在创建集群时，需要指定容器子网。系统会根据用户指定的子网和地址范围，推测出集群的最大节点规格和总 Pod 数量。
### 3.1.1 弹性网卡和 IP 地址容量的限制
* BCC实例与弹性网卡必须在同一可用区、同一VPC，可以在不同子网，挂载不同的安全组。
* 在一台实例上挂载多个弹性网卡不能提高实例内网带宽性能。
* 每个VPC可支持500个弹性网卡。如有需要，可以在配额中心申请提升配额。
* 单网卡上IP数量最少1个，最多40个。
* 云主机可挂载的弹性网卡数量为：当云主机cpu核数小于等于8时，最大可挂载的弹性网卡数量等于云主机cpu核数；当云主机cpu核数大于8时，最大可挂载8个弹性网卡
### 3.1.2 每个弹性网卡可绑定 IP 地址数量和资源之间的关系
#### 3.1.2.1 BCC 实例
BCC 实例的弹性网卡可绑定 IP 地址数量和资源之间的关系如下：
| 内存 |	IP数量| 
| --- | --- | 
| 1G |	2 |
| (1-8]G |	8 |
| (8-32]G |	16 |
| (32-64]G |	30 |
| 大于64G |	40 |

#### 3.1.2.2 BBC 实例
BBC 实例只有主网卡，主网卡可绑定的 IP 地址数量默认为 40 个。

#### 3.1.2.2 EBC 实例
EBC 实例在绑定 ENI 方式上有两种类型，分别为：
1. 可绑定 ENI 的实例，每个弹性网卡可绑定 IP 地址数量默认为 40 个。
2. 不可绑定 ENI 的实例，主网卡可绑定 IP 地址数量默认为 40 个。

### 3.1.3 集群最大节点和 Pod 规格
集群最大节点和 Pod 规格会按照用户选定的子网推算出来。具体推算的条件如下：
* 整个集群的资源规格是每个子网可承载的资源规格的累加。
* 用户选定子网时，仅使用子网剩余可用 IP 地址数量来推算集群最大节点和 Pod 规格。
* VPC 的子网默认第一个和最后一个都已被系统占用。
* 每个 ENI 所使用的 IP 地址包括了一个主 IP 和多个辅助 IP。Pod 通常仅使用辅助 IP 地址。
* 若单个子网剩余 IP 数不足以完整的填充一个 ENI 的配额，则会产生 IP 地址浪费。

以bcc.g5.c16m64机型为例最大支持 8 个 ENI，每个 ENI 支持 40 个辅助 IP。计算公式如下：
* 每个节点实际需创建 ENI 数，自动向上取整: `bind_eni_num = ceil( max_pod / (max_ip_per_eni - 1))`  
* 每个节点实际耗费的总 IP 地址数: `ip_max_per_node = bind_eni_num + max_pod`
* 某子网支持创建的节点数: `max_node = available_ip_num / ip_max_per_node`
* 某子网支持创建最大 eni 数: `max_eni = available_ip_num / max_ip_per_eni`

,每个子网可承载的资源规格速算表如下：
| 子网 IP 范围 | 最大可用 IP 地址数| 单机 Pod 数量| 最大节点数 | 单机 ENI 数量 |
| --- | --- | --- | 	--- | --- | 
| /24 |	254 个地址 |  32 个 Pod | 7 个节点 | 1 	|  |
| /24 |	254 个地址 |  64 个 Pod | 3 个节点 | 2 	| 
| /24 |	254 个地址 |  128 个 Pod | 1 个节点 | 4	| 
| /23 |	510 个地址 | 32 个 Pod 	| 15 个节点 |	1 |
| /23 |	510 个地址 | 64 个 Pod 	| 7 个节点 |	2 |
| /23 |	510 个地址 | 128 个 Pod 	| 3 个节点 |	4 |
| /23 |	510 个地址 | 256 个 Pod 	| 1 个节点 |	7 |
| /22 |	1022 个地址 |	32 个 Pod | | 30 个节点 | 1 |
| /22 |	1022 个地址 |	64 个 Pod | 15 个节点 |	2 |
| /22 |	1022 个地址 | 128 个 Pod 	| 7 个节点 |	4 |
| /22 |	1022 个地址 | 256 个 Pod 	| 3 个节点 |	7 |
| /22 |	1022 个地址 | 312 个 Pod 	| 3 个节点 |	8 |
| /21 |	2046 个地址 | 32 个 Pod 	| 62 个节点 |	1 |
| /21 |	2046 个地址 | 64 个 Pod 	| 31 个节点 |	2 |
| /21 |	2046 个地址 | 128 个 Pod 	| 15 个节点 |	4 |
| /21 |	2046 个地址 | 256 个 Pod 	| 7 个节点 |	7 |
| /21 |	2046 个地址 | 312 个 Pod 	| 6 个节点 |	8 |

> * 当节点有多个可选子网时，节点将选择一个能满足 ENI 最大 IP 地址数的子网。

### 3.2 ENI 池
#### 3.2.1 背景：BestEffort IP 地址池
经典容器网络使用 IP 地址作为基本分配单位，多个 ENI 同时抢占一个子网的 IP 地址，故称为BestEffort IP 地址池。
抢占 IP 地址池中每个 ENI 不知其他 ENI 的存在，故无法感知其他 ENI 分配的 IP 地址和 IP 容量信息。多个 ENI 在抢占子网 IP 剩余时，有很大概率出现 ENI IP 容量充足，但子网可用 IP 地址不足的情况。这种场景，是导致 VPC-ENI 模式经常导致用户的变更无法预测，甚至影响业务正常运行。

##### 3.2.1.1 背景：BestEffort IP 地址池的实现
抢占式 IP 地址池通过在 nrs 中标记 IP 地址池的弹性管理水位，实现 IP 地址池的弹性管理。下面是一个抢占 IP 地址池的示例：

``` yaml
apiVersion: cce.baidubce.com/v2
kind: NetResourceSet
metadata:
  name: 10.8.144.29
spec:
  addresses:
  - ip: 10.8.144.29
    type: InternalIP
  eni:
    availability-zone: zoneF
    enterpriseSecurityGroupList:
    - sg-xxxxx
    installSourceBasedRouting: true
    instance-type: bcc
    maxAllocateENI: 8
    maxIPsPerENI: 30
    preAllocateENI: 1
    routeTableOffset: 127
    subnet-ids:
    - sbn-test
    useMode: Secondary
    vpc-id: vpc-xxxx
  instance-id: i-99qd4iio
  ipam:
    pool:
      10.9.32.15:
        resource: eni-aaaa
        subnetID: sbn-test
      10.9.32.18:
        resource: eni-aaaa
        subnetID: sbn-test
      10.9.32.21:
        resource: eni-aaaa
        subnetID: sbn-test
      10.9.32.36:
        resource: eni-aaaa
        subnetID: sbn-test
    pre-allocate: 1
    min-allocate: 1
    max-above-watermark: 2
status:
  enis:
    eni-aaaa:
      cceStatus: ReadyOnNode
      id: eni-aaaa
      subnetId: sbn-test
      vpcStatus: inuse
```

* `pre-allocate`: `pre-allocate`定义了IPAMspec中必须可用于分配的IP地址数。它定义了立即可用的地址缓冲区。
* `min-allocate`: `min-allocate`是首次引导节点时必须分配的最小IP数。它定义了必须可用的地址的最小基套接字。到达该水印后，`pre-allocate`和`max-above-watermark`逻辑接管以继续分配IP。
* `max-allocate`: MaxAllocate是可以分配给节点的最大IP数。当已分配的IP数量接近此值时，考虑的预分配值将降至0，以避免试图分配超过定义的地址.
* `max-above-watermark`: `max-above-watermark` 是在达到预分配水位`pre-allocate`所需的地址之后要分配的最大地址数。超过预分配水位可以帮助减少分配IP的API调用数，例如，当分配新的ENI时，会分配尽可能多的辅助IP，限制`max-above-watermark`有助于减少IP的浪费。

可以看到通过上述 4 个参数控制的 IP 地址池的弹性管理的设计其实是非常灵活的，而且设计中考虑了性能、资源利用率等综合因素。例如调大 `max-above-watermark` 可以使IP 分配性能变好，调用 API 次数变少。而对于 IP 地址紧张的场景，也可以通过调整 `max-above-watermark` 参数，减少 IP 地址的浪费。
但是，上述 IP 地址池弹性管理的设计，存在一个比较大的问题：即没有考虑 ENI 的竞争关系，多个 ENI 同时竞争一个子网时，会导致某些 ENI 实际分配到的 IP 地址远小于自身的 IP 地址容量。

#### 3.2.2 Burstable IP地址池
为了解决上述 BestEffort IP 地址池弹性管理在资源竞争时的缺陷，CCE 引入了 Burstable IP 地址池。其核心思想是：**ENI一旦创建，立刻把 IP 地址容量全部填充满**。

优点：
* 解决了 BestEffort IP 地址池在资源竞争时的缺陷，ENI IP 地址容量没满时，子网的可用 IP 不足，ENI 无法再申请任何 IP。
##### 3.2.2.1 Burstable IP地址池的实现

``` yaml
apiVersion: cce.baidubce.com/v2
kind: NetResourceSet
metadata:
  name: 10.8.144.29
spec:
  addresses:
  - ip: 10.8.144.29
    type: InternalIP
  eni:
    availability-zone: zoneF
    enterpriseSecurityGroupList:
    - sg-xxxxx
    installSourceBasedRouting: true
    instance-type: bcc
    maxAllocateENI: 8
    maxIPsPerENI: 30
    preAllocateENI: 1
    # 添加 burstAllocate 参数
    burstableMehrfachENI: 1
    routeTableOffset: 127
    subnet-ids:
    - sbn-test
    useMode: Secondary
    vpc-id: vpc-xxxx
  instance-id: i-99qd4iio
  ipam:
    # 添加 max-allocate 参数，代表当前节点最大可以申请的 IP 地址数
    max-allocate: 128
    pool:
      10.9.32.15:
        resource: eni-aaaa
        subnetID: sbn-test
      10.9.32.18:
        resource: eni-aaaa
        subnetID: sbn-test
      10.9.32.21:
        resource: eni-aaaa
        subnetID: sbn-test
      10.9.32.36:
        resource: eni-aaaa
        subnetID: sbn-test
status:
  enis:
    eni-aaaa:
      cceStatus: ReadyOnNode
      id: eni-aaaa
      subnetId: sbn-test
      vpcStatus: inuse
```

* `burstableMehrfachENI`: 最小保留 ENI IP 容量倍数的空闲 IP 的数量。如果为 0 则代表不使用 Burstable ENI 模式。如果为 1，则代表始终保证一个 ENI  的 IP 地址处于就绪空闲状态（就绪 + IP 容量已充满）。
* `max-allocate`: 定义了IP 池最大的可用 IP 地址数，在 Burstable ENI 模式下，该值取自 kubelet 配置文件中的 `--max-pods`参数。
* CCE 默认使用 Burstable ENI模式，如果`burstableMehrfachENI`为 0 则代表不使用 Burstable ENI 模式。
##### 3.2.2.2 Burstable IP地址池最大 IP 地址计算
Burstable IP 地址池的最大可用 IP 地址数等于kubelet 配置文件中的 `--max-pods`参数，即单机最大 Pod 数。该参数由cce-network-agent的`nodediscovery`模块根据 kubelet 配置，实施数据采集。
* cce-network-agent 在程序启动时，会读取 kubelet 配置文件中的 `--max-pods`参数。
* 如果kubelet 配置文件中的 `--max-pods`参数不存在，则使用默认值 `110`。
* 如果在运行时更新了 kubelet 配置文件中的 `--max-pods`参数，则 cce-network-agent 不会重新读取该参数。需要重启 cce-network-agent 进程，才能读取到最新的 `--max-pods`参数。
* 节点分配的总 IP 数量不会超过 max-allocate + eniNum,给 Pod 可用的 IP 地址数量不会超过 max-allocate。

> * TODO: 如果新的 max-allocate 小于原 max-allocate，则会导致 ENI 数量减少。需要考虑是否支持该场景。


##### 3.2.2.2 Burstable IP地址池的创建 ENI 的时机
Burstable IP 地址池中有一个重要参数 `burstableMehrfachENI`，它定义了最小保留 ENI IP 容量倍数的空闲 IP 的数量。在Burstable IP 地址池模式下，始终保持最少一个 ENI IP 容量的 IP 地址是可用的。**如果总可用 IP 小于 `burstableMehrfachENI * (maxIPsPerENI - 1)`，则会立刻触发 ENI 扩容**，给IP池补充可用 IP 地址。 例如实例有 8 个 ENI，每个 ENI 最大 IP 容量为 30。
1. 如果已经有 1 个 IP 分配给了 Pod，即使有 28 个空闲 IP（ENI 的主 IP 占了一个 IP）， 也会创建第二个 ENI。此时总消耗 IP 为 1，总预备 IP 为 60 - 2 = 58。
2. 如果已经有 29 个 IP 分配给了 Pod，， 也会创建第二个 ENI。此时总消耗 IP 为 1，总预备 IP 为 60 - 2 = 58。

> 如果集群中存在会占用 IP 地址的 daemonset，对于任意节点来说，会出现最少申请 2 个 ENI的情况。

##### 3.2.2.3 Burstable IP地址池和节点最大 IP 地址的关系
`ipam.pool.max-allocate` 参数定义了 IP 池的最大可用 IP 地址数，在 Burstable ENI 模式下，该值取自 kubelet 配置文件。`max-allocate`值会发生以下几种情况：
1. 如果 `max-allocate`大于节点最大 IP 数(`maxAllocateENI * (maxIPsPerENI - 1)`)，则使用节点最大 IP 数。
2. 如果 `max-allocate`等于节点最大 IP 数(`maxAllocateENI * (maxIPsPerENI - 1)`)，则最终为申请 `maxAllocateENI` 个 ENI，并且每个 ENI 的辅助 IP 容量都充满。
3. 如果 `max-allocate`小于节点最大 IP 数`(maxAllocateENI * (maxIPsPerENI - 1)`，则最终申请 `ceil(max-allocate / (maxIPsPerENI - 1))`个 ENI，前面所有ENI辅助 IP 容量都充满，最后ENI辅助 IP 容量为 `max-allocate % maxIPsPerENI`。

> Burstable IP 地址池模式下，最后一个 ENI 的辅助 IP 数量决定与节点最大 IP 数量。所以最后一个 ENI 总是容量不满的。

##### 3.2.2.4 Burstable IP地址池下 IP 地址分配策略
为了保证 ENI 资源被合理使用，并为后续 ENI 回收做准备，我们需要尽可能保证 Pod 优先按 ENI 的创建顺序申请 IP。这样带来的好处如下：
1. 尽力使一个 ENI 所有的可用 IP 都被分配给 Pod。
2. 尽力保证新创建的 ENI 处于就绪空闲状态，方便执行 ENI 回收。

#### 3.2.3 故障发现和隔离

#### 3.2.4 BestEffort 向 Burstable IP地址池的升级
未来 CCE 将取消无规划的集群模式，将所有新建集群模式统一为 Burstable Guaranteed IP地址池。但现有VPC-ENI集群都在使用 BestEffort IP地址池，需要兼容升级。
为尽快能消除用户集群潜在风险，并能与用户已有集群平滑过渡，我们决定在升级时，将 BestEffort 和 Burstable Guaranteed IP地址池共存。平滑过渡方案如下：
1. 所有的新节点默认使用 Burstable Guaranteed IP地址池，`burstableMehrfachENI` 默认为 1。新节点自动切换为 BestEffort 和 Burstable Guaranteed IP地址池。
2. 已有节点，如果存在 `ipam.pool.max-above-watermark` 配置项，则在升级后，继续保留无规划模式的基本能力。
3. 已有节点，如果期望使用 Burstable Guaranteed IP地址池，需要把节点操作重新移除和移入集群。



* TODO:CCE 需要根据单机限定的最大 Pod 数，实时调整需要创建的 ENI 数量，以避免 IP 地址浪费。
* 为补齐用户期望单机最大 Pod 数，且避免 IP 地址浪费，当剩余用户最大期望 IP 地址小于单 ENI 最大辅助 IP 时，最后一个 ENI 仅申请剩余用户最大期望 IP 地址个 IP 地址。

### 3.3 其它系统的影响
#### 3.3.1 psts 的相互影响
psts 会使用跨子网分配 IP 的方式，为 Pod 分配 IP。由于 psts 的 IP 地址是跨子网的，并不会占用 ENI 原有的子网可用 IP 地址。所以在这里需要定义在 psts 场景下，如何善用 Burstable IP地址池。
##### 3.3.1.1 psts 会带来的问题
1. 跨子网 IP，也会导致 ENI 的 IP 地址容量被消耗。
2. 当跨子网 IP 地址被释放后，IP 地址池需要重新被 ENI 的子网填充，此时子网可能已经没有可用 IP 地址。
3. 当 ENI 的辅助 IP 容量已满时，跨子网 IP 需要替换原 ENI 的辅助 IP。
3.1 以替换跨子网辅助 IP 为目的的释放 ENI 的辅助 IP 后，需要防止 Burtable IP 地址池重新填充。
3.2 以替换跨子网辅助 IP 为目的的申请 ENI 的辅助 IP 前，需要防止 Burtable IP 地址池重新填充。

##### 3.3.1.2 子网 IP 预留功能的需求
一个子网可用 IP 地址是多个 ENI 的竞争资源，所以当 psts 触发了释放一个 ENI 的辅助 IP 时，需要保证子网中剩余的可用 IP 地址数大于等于 ENI 的IP 容量。这样当psts 把跨子网 IP 地址释放后，子网中剩余的可用 IP 地址数大于等于 ENI 的IP 容量，保证 ENI 最终依然是符合预期的。
为了实现子网预留的需求，我们需要在 ENI 创建时，预留一部分 IP 地址。当 psts 释放 ENI 的跨子网 IP 后，先把子网预留的 IP 地址释放掉，再给 ENI 补充剩余的 IP 地址。该功能需要 VPC 支持子网预留 IP 低的能力。