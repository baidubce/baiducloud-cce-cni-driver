# 2.0
v2 版本新架构，支持VPC-ENI 辅助IP和vpc路由。版本发布历史如下：

### 2.11 (2024/5/27)
新特性功能：
1. 新特性：容器内支持分配 RDMA 子网卡及 RDMA 辅助IP。

#### 2.11.2 [20240616]
1. [Bug] 修复 nrs 删除后，仍会不断错误重试的问题，持续新建 eni 的问题
2. [Bug] 修复 agent 重启后，restore 失败的问题
3. [Feature] VPC-ENI 增加 ehc 机型支持
4. [Optimize] cce-network-operator 增加 alloc-worker ，可配置并发处理 nrs 对象协程数
5. [Optimize] 优化 rdma 默认预申请 13 个 IP，最大空闲 IP 数为 104 个，避免频繁申请与释放 IP。
5. [Bug] 修复 rdma 释放 IP 时可能遇见的空指针问题
6. [Optimize] 优化 ebc 主机新建子网逻辑，非主网卡辅助 IP 模式时，不再校验主网卡子网
7. [Optimize] 去除多余 operator 日志，减少 operator 占用资源
8. [Optimize] 重启 agent 时，更新最新的子网信息，避免重启后子网信息不一致
9. [Optimize] 缩小 operator 中非必要的锁范围，提升 operator 处理性能
10. [Optimize] 增加触发器强制结束时间，避免单个节点卡主影响整体同步
11. [Optimize] 增加 HPC eni OpenAPI接口限流
12. [Optimize] 合并 rdma 和以太网资源同步器，减少重复同步带来的资源开销
13. [Feature] 增加 IP 释放回收控制开关，默认不会回收 ENI IP
14. [Bug] 修复节点偶发 maintainIPPool 方法得不到调用，节点无法同步问题
15. [Bug] 修复bcesync map 并发访问问题
16. [Optimize] 子网IP不足时，Event增加对应Request ID
17. [Feature] 增加 trigger 触发器指标粒度，细化到节点

#### 2.11.1 [20240611]
1. [Optimize] 更新RDMA IPPool MinAllocateIPs/PreAllocate/MaxAboveWatermark参数配置方式，与VPC-ENI保持一致方式
2. [Optimize] 保持RDMA网卡原始名称，不再rename RDMA网卡，避免Node上RDMA相关策略路由丢失
3. [Bug] 修复VPC路由下缺失ENISpec导致RDMA Discovery启动失败
4. [Bug] 修复ENI对象中RDMA网卡状态不同步问题
5. [Bug] 修复RDMA网卡最大IP数计算错误，优化报错信息
6. [Bug] 修复roce插件被误判为用户自定义插件

#### 2.11.0 [20240527]
1. [Feature] 新特性：容器内支持分配 RDMA 网卡。
    a. 支持单容器分配除VPC内以太网网卡之外，分配 RDMA 网卡的能力，分别支持 ERI 和 eRDMA 网卡。
    b. 容器使用RDMA网卡为共享模式，单Node内的所有使用 RDMA 资源的容器共享 RDMA网卡，每个RDMA网卡创建子设备并在容器内有独立的 RDMA IP。

### 2.10 (2024/03/05)
新特性功能：
1. 新特性：支持 VPC-ENI 模式下的 EBC 主网卡辅助 IP 模式。
2. 新特性：重构 CNI 配置文件管理逻辑，支持保留自定义 CNI 插件配置。
3. 新特性：增加对portmap 插件的支持，默认开启。
4. 新特性：VPC-ENI 支持自动获取节点 ENI 配额信息，去掉了自定义 ENI 配额的参数。
5. 新特性：支持通过在Node上添加 Annotation `network.cce.baidubce.com/node-eni-max-ips-num` 指定节点上 ENI 的最大辅助 IP 数量。

#### 2.10.4/2.10.5 [202405011]
1. [Bug] 修复 VPC-ENI 模式下，单机最大 IP 地址容量计算错误的问题

#### 2.10.3 [20240425]
1. [Bug] 修复 ResyncController 已经添加EventHandler 时，informer 重复添加处理器, psts 会收到重复事件会导致 IP 地址冲突的问题

#### 2.10.2 [20240403]
1. [Bug] 修复 vpc-route 模式下，重写 cni 文件错误问题

#### 2.10.1 [20240325]
1. [Bug] 修复 vpc-route 模式下，重启 operator 可能导致多个节点的 cidr 重复的问题
2. [Bug] 修复调用 bce sdk 出错时，可能出现的stack overflow，导致operator重启的问题
3. [Optimize] vpc-eni 增加 mac 地址合法性校验，避免误操作其它网卡

#### 2.10.0 （2024/03/05）
1. [Feature] VPC-ENI 支持自动获取节点 eni 配额信息，去掉了自定义 ENI 配额的参数。
2. [Feature] VPC-ENI 支持 ebc 主网卡辅助 IP 模式 
3. [Feature] VPC-ENI bbc 升级为主网卡辅助 IP 模式
4. [Optimize] 增加 CNI 插件日志持久化
5. [Feature] 重构 CNI 配置文件管理逻辑，支持保留自定义 CNI 插件配置
6. [Feature] 增加对portmap 插件的支持，默认开启
7. [Feature] 支持通过在Node上添加 Annotation `network.cce.baidubce.com/node-eni-max-ips-num` 指定节点上 ENI 的最大辅助 IP 数量。
8. [Bug] 修复arm64架构下，cni 插件无法执行的问题
9. [Optimize] 增加 BCE SDK 日志持久化
10. [Optimize] 优化去掉 bce sdk 的退避重试策略，避免频繁重试
11. [Optimize] 支持使用`default-api-timeout` 自定义参数指定 bce openAPI 超时时间

### 2.9 (2023/11/10)
新特性功能：
1. 新的 CRD: 支持集群级 psts ClusterPodSubnetTopologyStrategy (cpsts)，单个cpsts 可以控制作用于整个集群的 psts 策略。
2. CRD 字段变更: NetworkResourceSet 资源池增加了节点上 ENI 的异常状态，报错单机 IP 容量状态，整机 ENI 网卡状态。
3. 新特性: 支持ubuntu 22.04 操作系统，在容器网络环境下，定义 systemd-networkd 的 MacAddressPolicy 为 none。
4. 新特性：支持 pod 级 Qos

#### 2.9.5 [20240325]
1. [Bug] 修复 vpc-route 模式下，重启 operator 可能导致多个节点的 cidr 重复的问题
2. [Bug] 修复调用 bce sdk 出错时，可能出现的stack overflow，导致operator重启的问题

#### 2.9.4 [20240305]
1. [Feature] 支持 BBC 实例通过 Node 上增加 `network.cce.baidubce.com/node-eni-subnet` Anotation 配置指定节点上 ENI 的子网。 

#### 2.9.3 [20240228]
1. [Feature] cce-network-agent 自动同步节Node的Annoation信息到CRD中。
2. [Feature] 支持 EBC/BCC 实例通过 Node 上增加 `network.cce.baidubce.com/node-eni-subnet` Anotation 配置指定节点上 ENI 的子网。
3. [Feature] 新增`enable-node-annotation-sync`参数，默认关闭。
4. [Bug] 修正预申请 IP 时，可新建 ENI 的数量计算错误。

#### 2.9.2 [20240223]
1. [Bug] 修复arm64架构下，cni 插件无法执行的问题

#### 2.9.1 [20240115]
1. [Optimize] 优化NetResourceManager在接收事件时处理的锁，消除事件处理过程中 6 分钟延迟
2. [Optimize] 优化ENI状态机同步错误时，增加 3 次重试机会，消除因 ENI 状态延迟导致的 10 分钟就绪延迟
3. [Bug]修复 cce-network-agent 识别操作系统信息错误的问题
4. [Bug]修复cce-network-agent pod 被删除后，小概率导致 operator 空指针退出问题
5. [Bug]修复创建 eni 无法向 nrs 对象上打印 event 的问题

#### 2.9.0 [20240102]
1. [Optimize] 申请 IP 失败时,支持给出失败的原因.包括:
    a. 没有可用子网
    b. IP 地址池已满
    c. 节点 ENI 池已满
    d. 子网没有可用 IP
    e. IP 缓存池超限
2. [Feature] 新增 CRD: ClusterPodSubnetTopologyStrategy (cpsts), 用于控制集群级别的 psts 策略。
    a. 当前 crd 版本 cce.baidu.com/v2beta1
    b. cpsts 支持为所有符合namespaceSelector的 namespace 配置 psts 策略，并作为自身的子对象管理生命周期和状态。
3. [Feature]支持ubuntu 22.04 操作系统，在容器网络环境下，定义 systemd-networkd 的 MacAddressPolicy 为 none。
4. [Feature]支持 pod 级别的带宽控制，通过在 Pod 上设置 annotation 控制 Pod 级别的带宽。
    a. `kubernetes.io/ingress-bandwidth: 10M` 配置 Pod 的 ingress 带宽为 10M
    b. `kubernetes.io/egress-bandwidth: 10M` 配置 Pod 的 egress 带宽为 10M
5. [Feature]支持 pod 级别的 QoS，通过在 Pod 上设置 annotation 控制 Pod 的 QoS。
    a. `cce.baidubce.com/egress-priority: Guaranteed` 配置 Pod 的流量为 Guaranteed （最低延迟）优先级
    b. `cce.baidubce.com/egress-priority: Burstable` 配置 Pod 的流量为 Burstable （高优先级）
    c. `cce.baidubce.com/egress-priority: BestEffort` 配置 Pod 的 egress 流量为低优级
6. [Optimize] 修改 --bce-customer-max-eni 及 --bce-customer-max-ip 参数的逻辑，当参数非 0 时，强制生效
7. [Bug] 修复独占eni模式下容器网络命名空间挂载类型为tmpfs 时，无法读取netns的问题
8. [Feature] 增加override-cni-config开关，默认在 agent 启动时强制覆盖 cni 配置文件
9. [Feature] psts 复用 IP 时增加亲和性调度功能，保证同名 Pod 重复调度时，可以调度到同一可用区服用子网。
10. [Optimize] 优化并发创建 ENI 逻辑，避免在业务无需过多 IP 时并发创建过多 ENI
11. [Optimize] 优化 ENI 命名长度，限制为 64 字符
12. [Bug] 修复VPC-ENI 并发申请和释放IP 时，Pod 可能申请到过期的 IP 地址的问题

### 2.8 (2023/08/07)
1. 正式发布容器网络v2版本

#### 2.8.8 [20231227]
1. [Bug] VPC-ENI 并发申请和释放IP 时，Pod 可能申请到过期的 IP 地址

#### 2.8.7 [20231127]
1. [Bug] 修复 cce-network-v2-config 中 --bce-customer-max-eni 及 --bce-customer-max-ip 参数配置不生效；未限制并发创建 ENI ，并发下最大 ENI 数量可能超发

#### 2.8.6 [20231110]
1. [Bug] 优化 EndpointManager 在更新 endpoint 对象时不会超时的逻辑，且由于资源过期等问题会出现死循环的问题
2. [Optimize] 优化 operator 工作队列，支持自定义 worker 数量，加速事件处理
3. [Optimize] EndpointManager 核心工作流日志，把关键流程日志修改为 info 级别
4. [Optimize] 优化EndpointManager gc工作流，动态 IP 分配的 gc 时间设置为一周
5. [Optimize] 增加 ENI VPC 状态机流转时没有触发状态变更时重新入队时间，加速 ENI 就绪时间
6. [Optimize] 增加增删 ENI 状态变更事件，增加 ENI 的 VPC 非终态日志记录
7. [Optimize] 缺少 metaapi 时，记录相关事件
8. [Optimize] 当VPC路由满，记录相关事件

#### 2.8.5 [20241017]
1. [Optimize] 优化了 psts 分配 IP 时失败的回收机制，避免出现 IP 泄露
2. [Bug] 修复 vpc 路由模式下 nrs 标记 deleteTimeStamp 之后，由于 vpc 路由状态处于 released，nrs 的 finallizer 无法回收的问题
3. [Optimize] 优化创建 cep 的逻辑，当创建 cep 失败时，尝试主动删除并重新创建 cep

#### 2.8.4 [20230914]
1. [Bug] vpc-eni，修复在 centos 8等使用 NetworkManager 的操作系统发行版，当 ENI 网卡被重命名后，DHCP 删除 IP 导致 ENI 无法就绪的问题

#### 2.8.3 [20230904]
1. [Feature] 支持kubelet删除cni配置文件后，重新创建配置文件
2. [Feature] network-agent支持开启pprof，并获取mutex和block数据
1. [Optimize] 去掉network-agent在申请IP时的填充锁
2. [Bug] 修复network-agent的默认限流配置

#### 2.8.2 [20230829]
1. [Optimize] 提升创建ENI的性能，缩短nrs任务管理时间
2. [Optimize] 增加并发预创建eni的逻辑，当单机未达到预加载eni数时，并发创建eni
3. [Bug] 修复创建ENI时，使用eni名字查询eni对象为空，使每个ENI最少需要1m创建时间的问题

#### 2.8.1
1. [Bug] 修复sts Pod在未分配成功IP，被重新调度时，cep无法自动清理的问题
2. [Bug] 修复在多ENI场景下，eni策略路由会被误清理的问题

#### 2.8.0
1. [Feature] 支持Felix NetworkPolicy
2. [Feature] 支持virtual-kubelet节点跳过设置网络污点

### 2.7
#### 2.7.8
1. [Feature] 容器网络v2支持arm64架构

#### 2.7.7
1. [Feature] psts 支持新建eni网卡

#### 2.7.6
1. [Optimize] 修改Operator单次申请/释放IP数量，支持申请/释放部分成功
2. [Optimize] 优化IP申请和释放性能，增加多次部分申请IP机制

#### 2.7.5
1. [Bug] 修复endpoint manager死锁，导致psts的endpoint无法自动删除的问题
2. [Bug] 修复endpoint被误删后，nrs上IP地址无法回收，不能自动恢复的问题
3. [Bug] 修复 psts 指定IP范围数组越界问题

#### 2.7.4
1. [Bug] 修复双栈PSTS无法正常观测cep变更事件的问题
2. [Optimize] 更新CEP描述字段，增加IPs字段，展示cep所使用到的所有IP地址

#### 2.7.3
1. [Feature] ENI支持重启后重新配置

#### 2.7.2 
1. [Bug] 修复双栈集群中，IPv4和IPv6会形成地址数量差异的问题
2. [Bug] 修复cipam插件响应地址中没有IPv6地址的问题
3. [Bug] 修复NDP代理地址类型错误问题

#### 2.7.1
1. [Bug] 修复容器网络v2与cilium存在node.status竞争关系的问题
2. [Bug] 修复IPv6地址未在NetResourceSet中展示的问题

#### 2.7.0
1. [Feature] 增加对EBC机型支持

### 2.6
#### 2.6.0
1. [Optimize] 重构CCENode为NetResourceSet

### 2.5
#### 2.5.0
1. [Feature] 新增psts控制器，自动填充psts状态统计数据
2. [Feature] 新增pstswebhook，填充psts默认值，并校验psts合法性
3. [Feature] psts支持自动创建独占子网的自动创建和声明
4. [Optimize] 更换基础镜像
5. [Feature] 指定from/to两种策略路由
6. [Optimize] 更新cptp插件的地址管理机制，去掉了位于主机veth上的IP地址
7. [Optimize] 更换单机ENI接口名和路由表生成算法，单机最大可支持252个ENI

### 2.4
#### 2.4.0
1. [Feature] 新增VPC Route模式，支持中心化的自动为节点分配IPv4/IPv6 CIDR
2. [Optimize] vpc eni模式增加源策略路由
3. [Optimize]  eni模式增加arp/ndp代理
4. [Optimize] cipam 在vpc route模式下增加默认路由解析，使用宿主机的默认路由作为容器的默认路由
5. [Feature] 支持自定义单机最大ENI数和ENI最大IP数
6. [Feature] bbc 支持辅助IP
7. [Feature] VPC Route 支持动态路由地址分配
8. [Feature] VPC Route 增加VPC状态同步和回收机制
9. [Optimize] cptp 支持双向静态邻居表配置

### 2.3
#### 2.3.0
1. [Feature] 新增支持BCC PSTS指定子网分配IP策略。
2. [Feature] 支持IPv6固定IP
3. [Optimize] 升级BCE SDK，支持IPv6
4. [Feature] 增加cptp CNI插件，支持IPv6固定邻居
5. [Optimize] 移除pcb-ipam，增加更为通用的cipam

### 2.2
#### 2.2.0
1. [Feature] 新增支持外部扩展插件

### 2.1
#### 2.1.0 
1. [Feature] 新增支持独占ENI