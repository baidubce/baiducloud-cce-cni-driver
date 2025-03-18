# 2.0
v2 版本新架构，支持 VPC-ENI 辅助 IP 和 VPC 路由。版本发布历史如下：

### 2.12 (2024/06/28)
新特性功能：
1. 支持 Burstable ENI 池，有效避免子网 IP 资源紧张时，节点 ENI 池资源不足的问题。
2. 增加 ENI 安全组同步功能， 保持 CCE ENI 和节点安全组同步。
3. 增加节点网络配置集功能 NetResourceConfigSet，支持指定节点独立配置网络资源。
4. 增加对 HPAS 实例的支持

#### 2.12.17 [20250317]
1. [Optimize] NRS Manager Resync 同步逻辑由串行执行修改为并发执行
2. [Optimize] 弹性网卡和 NRS 状态逻辑由周期同步改为 ENI 成功创建后立即触发 NRS Manager Resync
3. [Bug] 修复 k8s-client-qps 和 k8s-client-burst 参数不生效的问题
4. [Bug] 修复 VPC-ENI 模式下，预期只纳管 CCE 创建的 ENI，而实际纳管了所有历史创建过的 ENI 的问题
5. [Optimize] CreateInterface 计算逻辑优化，首次 Create ENI 时，增加强制从 K8s 获取预创建的 ENI 对象（CCE 添加节点时提前完成 ENI 及 IP 的申请并创建 ENI 对象），避免重复创建/查询 ENI，提升节点扩容就绪性能
6. [Optimize] CreateInterface 计算逻辑优化，首次 Create ENI 时，增加强制从 IaaS 获取 ENI，避免非预期重复创建 ENI，减少 ENI 配额浪费，并提升节点就绪性能
7. [Optimize] ENI 状态机增加对 ownerReference 的判断，对非 NRS 管理的 ENI 对象跳过状态机检查
8. [Optimize] syncToAPIServer 新增 retry Trigger 逻辑，并将 IP 计算和同步到 k8s 最小重试间隔由 5s 改为 1s，解决高并发更新 NetworkResourceSet 失败导致必须走 Resync 周期间隔而引起的节点扩容就绪速度慢的问题
9. [Optimize] 优化 ENI 对象的更新重试逻辑，解决因 ENI 对象更新失败时而导致必须走 Resync 周期间隔而引起的扩容速度慢的问题
10. [Optimize] cce-network-agent list/watch 全量 NRS 对象改为只 watch 本节点的 NRS, 减少 watch 对象数量，提升启动性能
11. [Optimize] cce-network-agent 检查 IP 缓存水位需求是否已完成分配的间隔由 5s 修改为 1s，提升节点就绪性能
12. [Optimize] 修复 requireResync() 控制时许的置位逻辑，解决因 Resync 触发时未正确置位导致必须等待一个 resource-resync-interval 周期间隔而引起的节点扩容就绪速度慢的问题
13. [Optimize] 新增 Pod 创建过程中在容器网络配置完成后对 IP 可用性的检查过程，并实现可配置重试的 IP 检查过程

#### 2.12.16 [20250310]
1. [Optimize] ENI 状态机优化，去掉了所有无效 statENI 解决大规模集群 Ready 慢问题，去掉了状态机中错误的置位解决必须依赖 ReSync 的问题
2. [Bug] 修改自定义配置 api-rate-limit 限速不生效问题
3. [Optimize] 减少 Trigger MinInterval 参数，解决大规模集群扩容速度慢的问题
4. [Optimize] ENI 对象 VPCStatus 状态置位原子化，避免需要二次进状态机处理剩余状态
5. [Optimize] 新增 enable-api-rate-limit 配置选项，开启或关闭客户端 API 限速功能，默认不开启
6. [Bug] 修改 ENI 对象处理 finalizer 相关逻辑，解决清理 finalizer 不生效，导致 ENI 对象残留的问题
7. [Optimize] 所有对象的 ListWatch 新增 options.AllowWatchBookmarks = true 配置，避免因网络抖动导致的全量 List 操作
8. [Bug] 修复 RDMA 模式下，RDMA NRS 错用 bce-customer-max-ip 参数，导致 bce-customer-max-rdma-ip 参数不生效问题
9. [Bug] 修复开启 RDMA 场景下，当 Node Name 大于 48 个字符时 RDMA ENI 对象的 LabelSelectorValue 赋值错误，解决当 Node Name 超长时重启 cce-network-operator 和 cce-network-agent 后节点无法就绪的问题

#### 2.12.15 [20250227]
1. [Optimize] 优化 NetworkresourceSet.Spec.Addresses 的添加规则，避免重复添加节点 IP
2. [Bug] 修复 VPC-ENI 模式下，访问 ENI 接口失败时，误将 ENI 对象的 VPCStatus 标记为 None 的问题
3. [Optimize] 创建/lib/systemd/network/98-default.link文件，监控并持续维持其macAddressPolicy为None，解决该参数被意外修改后导致 veth 的 mac 地址被非预期变更的问题
4. [Bug] 修复 VPC-ENI 模式下，弹性网卡预挂载 eni-pre-allocate-num 配置在等于 eniQuota 时数量少一个的问题，增大重试次数避免 ENI 串行创建失败的问题

#### 2.12.14 [20250213]
1. [Bug] 修复 VPC-ENI 模式下的 remove ENI finalizer 更新逻辑，解决因节点删除时 ENI finalizer 未清理导致 ENI 对象残留的问题
2. [Bug] 修复 VPC-ENI 模式下，因启动时序导致在 cce-network-operator 启动过程中， PSTS 固定 IP 跨节点分配时可能导致的 panic 问题
3. [Bug] 修改开启 RDMA 场景下 List RDMA ENI 对象的 LabelSelectorValue 的拼装规则，解决当 Node Name 超长时因找不到对应的 RDMA ENI 对象而导致节点无法就绪的问题
4. [Bug] 修复开启 RDMA 场景下，操作 RDMA ENI 对象偶发 fatal error: concurrent map read and map write 问题
5. [Bug] 修复 VPC-ENI 模式下，在大量并发场景下偶发部分 ENI 的 MacAddress 为空，导致节点不 Ready 的情况
6. [Bug] 修复 VPC-ENI 模式下，PSTS 自定义 TTL 参数无法生效，一直为默认值 168h0m0s 的问题

#### 2.12.13 [20250122]
1. [Bug] 修复 ReuseIP CEP 跨 ENI 重用时， ENI 对象未更新导致 ReuseIP 在 ENI对象上残留的问题，解决使用固定 IP 的 Pod IP 非预期变更的问题
2. [Optimize] 优化 ubuntu22.04 中的 /lib/systemd/network/99-default.link 为 none，解决该参数被意外修改后导致 veth 的 mac 地址被非预期变更的问题
3. [Bug] 修复 RDMA 场景下，请求 HPC 接口返回结果为 nil 时的空指针问题
4. [Bug] 修复 VPC-ENI 模式下开启 RDMA 时的 Romote ENI Syncer 逻辑，纳管 CCE 历史创建的 ENI 时避免将 ENI 网卡错误识别为 RDMA ENI 导致 RDMA 网卡无法正常使用的问题

#### 2.12.12 [20250121]
1. [Bug] 修复 NetResourceConfigSet 对应的 ExtCniPlugins 字段修改不生效问题，支持节点维度的自定义自定义插件 ExtCniPlugins 字段配置
2. [Feature] 增加对 HPAS 实例的支持

#### 2.12.11 [20241227]
1. [Bug] 修复 VPC-ENI 模式下，弹性网卡预挂载 eni-pre-allocate-num 配置不生效的问题
2. [Bug] 修改开启 RDMA 场景下 RDMA ENI 对象本地缓存过期状态相关逻辑，解决因 resync nrs timeout 而导致的新增 RDMA 节点初始化慢，大规模集群扩容速度慢的问题
3. [Optimize] 修改开启 RDMA 场景下 RDMA NetResourceSet 对象拼装规则，以及 ENI 对象的 LabelSelectorValue 的拼装规则，防止 RDMA NetResourceSet 名字超过限定值 253，防止 ENI 对象的 LabelSelectorValue 超过限定值 63，解决因 Node Name 超长而导致的 cce-network-agent panic 问题
4. [Optimize] 修改 RDMA ENI 对象更新逻辑，解决因 NodeName 变更时 ENI 对象未正常销毁而导致的 RDMA ENI 对象无法被更新而导致的节点无法就绪的问题
5. [Optimize] 优化开启 RDMA 模式时的 RDMA ENI 状态机处理逻辑，支持非终态 RDMA ENI 的处理流程，避免非终态状态 RDMA ENI 卡住节点 NotReady 无法恢复的问题
6. [Optimize] 优化开启 RDMA 模式时，对 HPC OpenAPI 的请求逻辑，大幅降低请求频率，降低大规模集群下的 OpenAPI 请求压力
7. [Bug] 修复创建 Node Interface 对象时因初始值未判断而导致的出现 Instance is out of interfaces 导致节点就绪慢的问题

#### 2.12.10 [20241213]
1. [Optimize] 优化 VPC-ENI 模式下的 veth 单机路由规则目的地址存在冲突的判断条件，解决残留本地路由规则时创建的 Pod 容器网络不通的问题
2. [Optimize] 禁用 VPC-ENI 模式下的 IPv6 DHCP 时使用的网卡名修改为udev生成的原始名字，避免生成的网卡配置文件导致虚拟机重启时 network.service 服务启动失败
3. [Bug] 更新 VPC-ENI 模式下的 ENI 状态机处理逻辑，修复当节点移出集群时，ENI 对象残留的问题
4. [Bug] 修复 VPC-ENI 模式下的新建 ENI 网卡 borrow IP 地址时，borrow task ENIID 填写错误导致并发创建时可能引起的 borrow IP 计算错误
5. [Optimize] 优化 VPC-ENI 模式下的 Romote ENI Syncer 逻辑，纳管 CCE 历史创建的 ENI，避免因历史创建的 ENI 无法被使用而占用 Node eniQuota 的问题
6. [Optimize] 优化 VPC-ENI 模式下的 ENI 状态机处理逻辑，支持非终态 ENI 的处理流程，避免因状态机异常中断、重启丢失内存状态等原因导致的 ENI 状态处于异常的非终态而导致的该 ENI 无法正常使用的问题
7. [Bug] PSTS 增加对 CEP TTL 未过期时直接移除 Node 或 ENI 导致 CEP 后续 TTL 过期后因无对应的 NetworkResourceSet 或 ENI 而无法正常删除时的清理逻辑
8. [Bug] 修复 VPC-ENI 模式下 EBC ENI Quota 为 0 时，启用主网卡辅助IP的 ENI 赋值逻辑，避免因主网卡 ENI MacAddress 为空导致 cce-network-agent 无法启动的问题
9. [Bug] 修改 VPC-ENI 模式下 ENI 对象本地缓存过期状态相关逻辑，解决因 resync nrs timeout 而导致的新增节点初始化慢，大规模集群扩容速度慢的问题

#### 2.12.9 [20241121]
1. [Bug] 修复 agent 在初始化 ENI 缺少 mac 地址时，会给 lo 网卡重命名的问题
2. [Optimize] 修复 Node 不存在的异常场景时 Operator getEniQuota panic 问题

#### 2.12.8 [20240924]
1. [Bug] 增加 ENI 主 IP 获取流程，避免新节点缺少主 IP 无法就绪的问题
2. [Bug] 增加 EBC 主网卡 IP 查询流程，避免新节点缺少主 IP 无法就绪的问题
3. [Optimize] 增加 restore IP 流程的严格模式，严格模式下如果无法 restore IP 时，触发 agent 重启
4. [Optimize] 优化 IP 借用规则，当主网卡已有辅助 IP 时，减少借用 IP 地址的数量
5. [Bug] 修复 EBC 主网卡重复创建 ENI 的问题

#### 2.12.7 [20240923]
1. [Optimize] PSTS 增加对 CEP TTL 未过期时直接移除 ENI 导致 CEP 后续 TTL 过期后因无对应 ENI 而无法正常删除时的清理逻辑
2. [Optimize] 增加 ENI 同步时不一致信息的差异对比日志，方便出现 ENI 数据不一致时排查问题
3. [Optimize] 去掉 ERI 的独立同步逻辑，复用 ERI 和 ENI 的同步流程
4. [Optimize] 去掉 Underlay RDMA 的独立同步逻辑，创建 underlay RDMA 网卡后，状态不再变更
5. [Optimize] 去掉 BBC/EBC 主网卡状态机同步流程，ENI 状态不再变更
6. [Optimize] 优化 ENI 同步流程，仅对 ENI 状态变更时同步，减少 IP 地址变更同步的消耗

#### 2.12.6 [20240913]
1. [Bug] 修复在 RDMA 场景查询子网为空，打印错误日志的问题
2. [Bug] 修复在 VPC-Route 模式下，开启 RDMA 后，重启 Operator 进程会为 RDMA NRS 分配 podCIDR 的问题。
3. [Optimize] PSTS 增加对未被 CCE 管理的跨子网 IP 地址的清理逻辑

#### 2.12.5 [20240829]
1. [Bug] 修复在 2.12.4 版本中同步 ENI 状态时丢失 IPv6 地址的问题。
2. [Bug] 修复有多个 ENI 都存在待释放 IP 时，多次查询 ENI 顺序不一致影响 IP 标记流程导致无法释放 IP 的问题
3. [Bug] 修复重复 Update NRS, 导致 Operation cannot be fulfilled 的问题

#### 2.12.4 [2024/08/12]
1. [Bug] 修复在 prepareIPs 阶段多余检查 borrowed subnet 是否有可借用 IP，影响正常 IP 地址申请的问题。
2. [Bug] 修复 VPC-ENI 模式会申请超过 max-pods 个 IP 的问题
2. [Optimize] 优化 prepareIPs 阶段对子网查询的逻辑，当无法获取子网信息时，不再继续进行后续操作。
3. [Optimize] 优化 prepareIPs 阶段对子网查询的逻辑，当 BCC 实例的 ENI 处于非 inuse 状态时，拒绝执行 IP 预备任务。

#### 2.12.3 [20240730]
1. [Bug] 修复 CPSTS 配置 namespaceSelector 时误判断 Selector 导致没有配置 namespaceSelector 时的空指针问题及 namespaceSelector 在没有配置 Selector 时无法生效的问题
2. [Optimize] 优化 PSTS 在没有填写子网 IP 选择策略时的本地 IP 申请器的默认工作区间，避免没有填写 IP 地址族时无法申请 IP 的问题
3. [Bug] 修复 cilium ipam 保留 IP 时无法保留首个 IP 的问题

#### 2.12.2 [2024/07/24]
1. [Feature] 支持 borrowed subnet 可观测，新增 cce_subnet_ips_guage 指标代表子网可用 IP 地址数量
2. [Optimize] borrowed subnet 支持定时同步能力，避免因单次 IP 计算错误，导致错误借用未归还的问题。
3. [Optimize] 更新子网可用 IP 借用语义，单个 ENI 从子网借用 IP 地址数以最新一次为准

#### 2.12.1 [2024/07/02]
1. [Bug] 修复 BBC 机型开启 burstable ENI 时，初始化会导致空指针的问题
2. [Bug] 修复 BBC ENI 不返回实例 ID 时，无法选中 ENI 的，影响节点就绪时间的问题

#### 2.12.0 [2024/06/28]
1. [Feature] 支持 Burstable ENI 池，有效避免子网 IP 资源紧张时，节点 ENI 池资源不足的问题。
2. [Feature] 新增子网不足导致 ENI 创建失败 metrics 指标
3. [Feature] 增加 ENI 安全组同步功能， 保持CCE ENI 和节点安全组同步
4. [Feature] 优化 Pod 调度算法，增加节点 IP Capacity 自动适配，避免节点 IP 地址资源浪费
5. [Feature] 增加节点网络配置集功能 NetResourceConfigSet，支持指定节点独立配置网络资源
6. [Optimize] 修复 PSTS 对象在使用 enableReuseIPAddress时可能更新 CEP 时 Addressing 为空，不能记录错误信息的问题
7. [Optimize] 优化 Operator 事件积压问题，避免事件长期超时积压
8. [Optimize] agent 优化 IP 地址 gc 算法，在达到 gc 周期后，支持按照 IP 地址清理已变更 CEP 遗留地址的能力
9. [Optimize] 将动态 CEP 与 NRS 生命周期绑定，减少 agent 被杀死的缩容时，遗留的 CEP 对象数
10. [Optimize] 优化 RDMA IP 申请流程，避免 RDMA 使用固定 IP 的 CEP

### 2.11 (2024/5/27)
新特性功能：
1. 新特性：容器内支持分配 RDMA 子网卡及 RDMA 辅助IP。

#### 2.11.9 [20241213]
1. [Bug] 修改 VPC-ENI 模式下 ENI 对象本地缓存过期状态相关逻辑，解决因 resync nrs timeout 而导致的新增节点初始化慢，大规模集群扩容速度慢的问题

#### 2.11.8 [20241101]
1. [Bug] 修复 agent 在初始化 ENI 缺少 mac 地址时，会给 lo 网卡重命名的问题

#### 2.11.7 [20241031]
1. [Optimize] 增加 ENI 主 IP 获取流程，避免新节点缺少主 IP 无法就绪的问题

#### 2.11.6 [20240924]
1. [Bug] 修复 ENI 同步不支持 EHC 的问题

#### 2.11.5 [20240920]
1. [Optimize] 增加 ENI 同步时不一致信息的差异对比日志，方便出现 ENI 数据不一致时排查问题
2. [Optimize] 去掉 ERI 的独立同步逻辑，复用 ERI 和 ENI 的同步流程
3. [Optimize] 去掉 Underlay RDMA 的独立同步逻辑，创建 underlay RDMA 网卡后，状态不再变更
4. [Optimize] 去掉 BBC/EBC 主网卡状态机同步流程，ENI 状态不再变更
5. [Optimize] 优化 ENI 同步流程，仅对 ENI 状态变更时同步，减少 IP 地址变更同步的消耗

#### 2.11.4 [20240823]
1. [Bug] 修复有多个 ENI 都存在待释放 IP 时，多次查询 ENI 顺序不一致影响 IP 标记流程导致无法释放 IP 的问题
2. [Bug] 修复重复 Update NRS,导致 Operation cannot be fulfilled的问题

#### 2.11.3 [20240628]
1. [Feature] `--endpoint-gc-interval` 增加控制 agent 更新 NRS 的最小间隔时间
2. [Optimize] 优化对 ENI 重启事件的处理逻辑，优化 agent 重启速度
3. [Optimize] 对 BCE ENI 的 IP 地址做重排序，减少非必要 ENI 更新事件
4. [Bug] 优化手工删除subnet情况下StartSynchronizingSubnet可能遇到空指针

#### 2.11.2 [20240616]
1. [Bug] 修复 NRS 删除后，仍会不断错误重试的问题，持续新建 ENI 的问题
2. [Bug] 修复 agent 重启后，restore 失败的问题
3. [Feature] VPC-ENI 增加 ehc 机型支持
4. [Optimize] cce-network-operator 增加 alloc-worker ，可配置并发处理 NRS 对象协程数
5. [Optimize] 优化 RDMA 默认预申请 13 个 IP，最大空闲 IP 数为 104 个，避免频繁申请与释放 IP。
5. [Bug] 修复 RDMA 释放 IP 时可能遇见的空指针问题
6. [Optimize] 优化 ebc 主机新建子网逻辑，非主网卡辅助 IP 模式时，不再校验主网卡子网
7. [Optimize] 去除多余 Operator 日志，减少 Operator 占用资源
8. [Optimize] 重启 agent 时，更新最新的子网信息，避免重启后子网信息不一致
9. [Optimize] 缩小 Operator 中非必要的锁范围，提升 Operator 处理性能
10. [Optimize] 增加触发器强制结束时间，避免单个节点卡主影响整体同步
11. [Optimize] 增加 HPC ENI OpenAPI接口限流
12. [Optimize] 合并 RDMA 和以太网资源同步器，减少重复同步带来的资源开销
13. [Feature] 增加 IP 释放回收控制开关，默认不会回收 ENI IP
14. [Bug] 修复节点偶发 maintainIPPool 方法得不到调用，节点无法同步问题
15. [Bug] 修复 bcesync map 并发访问问题
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
3. 新特性：增加对 portmap 插件的支持，默认开启。
4. 新特性：VPC-ENI 支持自动获取节点 ENI 配额信息，去掉了自定义 ENI 配额的参数。
5. 新特性：支持通过在 Node 上添加 Annotation `network.cce.baidubce.com/node-eni-max-ips-num` 指定节点上 ENI 的最大辅助 IP 数量。

#### 2.10.4/2.10.5 [202405011]
1. [Bug] 修复 VPC-ENI 模式下，单机最大 IP 地址容量计算错误的问题

#### 2.10.3 [20240425]
1. [Bug] 修复 ResyncController 已经添加EventHandler 时，informer 重复添加处理器, PSTS 会收到重复事件会导致 IP 地址冲突的问题

#### 2.10.2 [20240403]
1. [Bug] 修复 VPC-Route 模式下，重写 cni 文件错误问题

#### 2.10.1 [20240325]
1. [Bug] 修复 VPC-Route 模式下，重启 Operator 可能导致多个节点的 cidr 重复的问题
2. [Bug] 修复调用 BCE SDK 出错时，可能出现的stack overflow，导致 operator 重启的问题
3. [Optimize] VPC-ENI 增加 mac 地址合法性校验，避免误操作其它网卡

#### 2.10.0 （2024/03/05）
1. [Feature] VPC-ENI 支持自动获取节点 ENI 配额信息，去掉了自定义 ENI 配额的参数。
2. [Feature] VPC-ENI 支持 ebc 主网卡辅助 IP 模式 
3. [Feature] VPC-ENI BBC 升级为主网卡辅助 IP 模式
4. [Optimize] 增加 CNI 插件日志持久化
5. [Feature] 重构 CNI 配置文件管理逻辑，支持保留自定义 CNI 插件配置
6. [Feature] 增加对portmap 插件的支持，默认开启
7. [Feature] 支持通过在Node上添加 Annotation `network.cce.baidubce.com/node-eni-max-ips-num` 指定节点上 ENI 的最大辅助 IP 数量。
8. [Bug] 修复arm64架构下，cni 插件无法执行的问题
9. [Optimize] 增加 BCE SDK 日志持久化
10. [Optimize] 优化去掉 BCE SDK 的退避重试策略，避免频繁重试
11. [Optimize] 支持使用`default-api-timeout` 自定义参数指定 BCE openAPI 超时时间

### 2.9 (2023/11/10)
新特性功能：
1. 新的 CRD: 支持集群级 PSTS ClusterPodSubnetTopologyStrategy (CPSTS)，单个 CPSTS 可以控制作用于整个集群的 PSTS 策略。
2. CRD 字段变更: NetworkResourceSet 资源池增加了节点上 ENI 的异常状态，报错单机 IP 容量状态，整机 ENI 网卡状态。
3. 新特性: 支持ubuntu 22.04 操作系统，在容器网络环境下，定义 systemd-networkd 的 MacAddressPolicy 为 none。
4. 新特性：支持 pod 级 Qos

#### 2.9.5 [20240325]
1. [Bug] 修复 VPC-Route 模式下，重启 Operator 可能导致多个节点的 cidr 重复的问题
2. [Bug] 修复调用 BCE SDK 出错时，可能出现的stack overflow，导致 operator 重启的问题

#### 2.9.4 [20240305]
1. [Feature] 支持 BBC 实例通过 Node 上增加 `network.cce.baidubce.com/node-eni-subnet` Anotation 配置指定节点上 ENI 的子网。 

#### 2.9.3 [20240228]
1. [Feature] cce-network-agent 自动同步节Node的Annoation信息到CRD中。
2. [Feature] 支持 EBC/BCC 实例通过 Node 上增加 `network.cce.baidubce.com/node-eni-subnet` Anotation 配置指定节点上 ENI 的子网。
3. [Feature] 新增`enable-node-annotation-sync`参数，默认关闭。
4. [Bug] 修正预申请 IP 时，可新建 ENI 的数量计算错误。

#### 2.9.2 [20240223]
1. [Bug] 修复 arm64 架构下，CNI 插件无法执行的问题

#### 2.9.1 [20240115]
1. [Optimize] 优化 NetResourceManager 在接收事件时处理的锁，消除事件处理过程中 6 分钟延迟
2. [Optimize] 优化 ENI 状态机同步错误时，增加 3 次重试机会，消除因 ENI 状态延迟导致的 10 分钟就绪延迟
3. [Bug] 修复 cce-network-agent 识别操作系统信息错误的问题
4. [Bug] 修复 cce-network-agent pod 被删除后，小概率导致 Operator 空指针退出问题
5. [Bug] 修复创建 ENI 无法向 NRS 对象上打印 event 的问题

#### 2.9.0 [20240102]
1. [Optimize] 申请 IP 失败时,支持给出失败的原因.包括:
    a. 没有可用子网
    b. IP 地址池已满
    c. 节点 ENI 池已满
    d. 子网没有可用 IP
    e. IP 缓存池超限
2. [Feature] 新增 CRD: ClusterPodSubnetTopologyStrategy (CPSTS), 用于控制集群级别的 PSTS 策略。
    a. 当前 CRD 版本 cce.baidu.com/v2beta1
    b. CPSTS 支持为所有符合 namespaceSelector 的 namespace 配置 PSTS 策略，并作为自身的子对象管理生命周期和状态。
3. [Feature]支持ubuntu 22.04 操作系统，在容器网络环境下，定义 systemd-networkd 的 MacAddressPolicy 为 none。
4. [Feature]支持 pod 级别的带宽控制，通过在 Pod 上设置 annotation 控制 Pod 级别的带宽。
    a. `kubernetes.io/ingress-bandwidth: 10M` 配置 Pod 的 ingress 带宽为 10M
    b. `kubernetes.io/egress-bandwidth: 10M` 配置 Pod 的 egress 带宽为 10M
5. [Feature]支持 pod 级别的 QoS，通过在 Pod 上设置 annotation 控制 Pod 的 QoS。
    a. `cce.baidubce.com/egress-priority: Guaranteed` 配置 Pod 的流量为 Guaranteed （最低延迟）优先级
    b. `cce.baidubce.com/egress-priority: Burstable` 配置 Pod 的流量为 Burstable （高优先级）
    c. `cce.baidubce.com/egress-priority: BestEffort` 配置 Pod 的 egress 流量为低优级
6. [Optimize] 修改 --bce-customer-max-eni 及 --bce-customer-max-ip 参数的逻辑，当参数非 0 时，强制生效
7. [Bug] 修复独占 ENI 模式下容器网络命名空间挂载类型为 tmpfs 时，无法读取 netns 的问题
8. [Feature] 增加 override-cni-config 开关，默认在 agent 启动时强制覆盖 CNI 配置文件
9. [Feature] PSTS 复用 IP 时增加亲和性调度功能，保证同名 Pod 重复调度时，可以调度到同一可用区服用子网。
10. [Optimize] 优化并发创建 ENI 逻辑，避免在业务无需过多 IP 时并发创建过多 ENI
11. [Optimize] 优化 ENI 命名长度，限制为 64 字符
12. [Bug] 修复 VPC-ENI 并发申请和释放IP 时，Pod 可能申请到过期的 IP 地址的问题

### 2.8 (2023/08/07)
1. 正式发布容器网络v2版本

#### 2.8.8 [20231227]
1. [Bug] VPC-ENI 并发申请和释放IP 时，Pod 可能申请到过期的 IP 地址

#### 2.8.7 [20231127]
1. [Bug] 修复 cce-network-v2-config 中 --bce-customer-max-eni 及 --bce-customer-max-ip 参数配置不生效；未限制并发创建 ENI ，并发下最大 ENI 数量可能超发

#### 2.8.6 [20231110]
1. [Bug] 优化 EndpointManager 在更新 Endpoint 对象时不会超时的逻辑，且由于资源过期等问题会出现死循环的问题
2. [Optimize] 优化 Operator 工作队列，支持自定义 worker 数量，加速事件处理
3. [Optimize] EndpointManager 核心工作流日志，把关键流程日志修改为 info 级别
4. [Optimize] 优化 EndpointManager gc工作流，动态 IP 分配的 gc 时间设置为一周
5. [Optimize] 增加 ENI VPC 状态机流转时没有触发状态变更时重新入队时间，加速 ENI 就绪时间
6. [Optimize] 增加增删 ENI 状态变更事件，增加 ENI 的 VPC 非终态日志记录
7. [Optimize] 缺少 metaapi 时，记录相关事件
8. [Optimize] 当VPC路由满，记录相关事件

#### 2.8.5 [20241017]
1. [Optimize] 优化了 PSTS 分配 IP 时失败的回收机制，避免出现 IP 泄露
2. [Bug] 修复 VPC 路由模式下 NRS 标记 deleteTimeStamp 之后，由于 VPC 路由状态处于 released，NRS 的 finallizer 无法回收的问题
3. [Optimize] 优化创建 CEP 的逻辑，当创建 CEP 失败时，尝试主动删除并重新创建 CEP

#### 2.8.4 [20230914]
1. [Bug] VPC-ENI，修复在 CentOS 8等使用 NetworkManager 的操作系统发行版，当 ENI 网卡被重命名后，DHCP 删除 IP 导致 ENI 无法就绪的问题

#### 2.8.3 [20230904]
1. [Feature] 支持 kubelet 删除 CNI 配置文件后，重新创建配置文件
2. [Feature] network-agent 支持开启pprof，并获取 mutex 和 block 数据
1. [Optimize] 去掉 network-agent 在申请 IP 时的填充锁
2. [Bug] 修复 network-agent 的默认限流配置

#### 2.8.2 [20230829]
1. [Optimize] 提升创建ENI的性能，缩短 NRS 任务管理时间
2. [Optimize] 增加并发预创建ENI的逻辑，当单机未达到预加载ENI数时，并发创建 ENI
3. [Bug] 修复创建 ENI 时，使用ENI名字查询 ENI 对象为空，使每个 ENI 最少需要 1m 创建时间的问题

#### 2.8.1
1. [Bug] 修复 STS Pod 在未分配成功 IP，被重新调度时，CEP 无法自动清理的问题
2. [Bug] 修复在多 ENI 场景下，ENI 策略路由会被误清理的问题

#### 2.8.0
1. [Feature] 支持 Felix NetworkPolicy
2. [Feature] 支持 virtual-kubelet 节点跳过设置网络污点

### 2.7
#### 2.7.8
1. [Feature] 容器网络v2 支持 arm64 架构

#### 2.7.7
1. [Feature] PSTS 支持新建 ENI 网卡

#### 2.7.6
1. [Optimize] 修改 Operator 单次申请/释放 IP 数量，支持申请/释放部分成功
2. [Optimize] 优化 IP 申请和释放性能，增加多次部分申请 IP 机制

#### 2.7.5
1. [Bug] 修复 Endpoint manager 死锁，导致 PSTS 的 Endpoint 无法自动删除的问题
2. [Bug] 修复 Endpoint 被误删后，NRS 上 IP 地址无法回收，不能自动恢复的问题
3. [Bug] 修复 PSTS 指定IP范围数组越界问题

#### 2.7.4
1. [Bug] 修复双栈 PSTS 无法正常观测 CEP 变更事件的问题
2. [Optimize] 更新 CEP 描述字段，增加 IPs 字段，展示 CEP 所使用到的所有 IP 地址

#### 2.7.3
1. [Feature] ENI 支持重启后重新配置

#### 2.7.2 
1. [Bug] 修复双栈集群中，IPv4 和 IPv6 会形成地址数量差异的问题
2. [Bug] 修复 cipam 插件响应地址中没有 IPv6 地址的问题
3. [Bug] 修复 NDP 代理地址类型错误问题

#### 2.7.1
1. [Bug] 修复容器网络v2 与 cilium 存在 node.status 竞争关系的问题
2. [Bug] 修复 IPv6 地址未在 NetResourceSet 中展示的问题

#### 2.7.0
1. [Feature] 增加对 EBC 机型支持

### 2.6
#### 2.6.0
1. [Optimize] 重构 CCENode 为 NetResourceSet

### 2.5
#### 2.5.0
1. [Feature] 新增 PSTS 控制器，自动填充 PSTS 状态统计数据
2. [Feature] 新增 PSTS webhook，填充 PSTS 默认值，并校验 PSTS 合法性
3. [Feature] PSTS 支持自动创建独占子网的自动创建和声明
4. [Optimize] 更换基础镜像
5. [Feature] 指定 from/to 两种策略路由
6. [Optimize] 更新 cptp 插件的地址管理机制，去掉了位于主机 veth 上的 IP 地址
7. [Optimize] 更换单机 ENI 接口名和路由表生成算法，单机最大可支持 252个 ENI

### 2.4
#### 2.4.0
1. [Feature] 新增VPC Route模式，支持中心化的自动为节点分配IPv4/IPv6 CIDR
2. [Optimize] VPC-ENI 模式增加源策略路由
3. [Optimize] VPC-ENI 模式增加 arp/ndp 代理
4. [Optimize] cipam 在 VPC-Route 模式下增加默认路由解析，使用宿主机的默认路由作为容器的默认路由
5. [Feature] 支持自定义单机最大ENI数和ENI最大IP数
6. [Feature] BBC 支持辅助IP
7. [Feature] VPC Route 支持动态路由地址分配
8. [Feature] VPC Route 增加VPC状态同步和回收机制
9. [Optimize] cptp 支持双向静态邻居表配置

### 2.3
#### 2.3.0
1. [Feature] 新增支持 BCC PSTS 指定子网分配 IP 策略。
2. [Feature] 支持 IPv6 固定 IP
3. [Optimize] 升级 BCE SDK，支持 IPv6
4. [Feature] 增加 cptp CNI 插件，支持 IPv6 固定邻居
5. [Optimize] 移除 pcb-ipam，增加更为通用的 cipam

### 2.2
#### 2.2.0
1. [Feature] 新增支持外部扩展插件

### 2.1
#### 2.1.0 
1. [Feature] 新增支持独占 ENI