# 2.0
v2 版本新架构，支持VPC-ENI 辅助IP和vpc路由。版本发布历史如下：

### 2.8 (2023/08/07)
#### 2.8.5 [20241017]
1. [优化] 优化了 psts 分配 IP 时失败的回收机制，避免出现 IP 泄露
2. [BUGFIX] 修复 vpc 路由模式下 nrs 标记 deleteTimeStamp 之后，由于 vpc 路由状态处于 released，nrs 的 finallizer 无法回收的问题
#### 2.8.4 [20230914]
1. [BUG] vpc-eni，修复在 centos 8等使用 NetworkManager 的操作系统发行版，当 ENI 网卡被重命名后，DHCP 删除 IP 导致 ENI 无法就绪的问题
#### 2.8.3 [20230904]
1. [Feature]支持kubelet删除cni配置文件后，重新创建配置文件
2. [Feature]network-agent支持开启pprof，并获取mutex和block数据
1. [优化]去掉network-agent在申请IP时的填充锁
2. [BUG]修复network-agent的默认限流配置
#### 2.8.2 [20230829]
1. [优化]提升创建ENI的性能，缩短nrs任务管理时间
2. [优化]增加并发预创建eni的逻辑，当单机未达到预加载eni数时，并发创建eni
3. [BUG]修复创建ENI时，使用eni名字查询eni对象为空，使每个ENI最少需要1m创建时间的问题

#### 2.8.1
1. [BUG]修复sts Pod在未分配成功IP，被重新调度时，cep无法自动清理的问题
2. [BUG]修复在多ENI场景下，eni策略路由会被误清理的问题

#### 2.8.0
1. 支持Felix NetworkPolicy
2. 支持virtual-kubelet节点跳过设置网络污点

### 2.7
#### 2.7.8
1. 容器网络v2支持arm64架构

#### 2.7.7
1. psts 支持新建eni网卡

#### 2.7.6
1. 修改Operator单次申请/释放IP数量，支持申请/释放部分成功
2. 优化IP申请和释放性能，增加多次部分申请IP机制

#### 2.7.5
1. 修复endpoint manager死锁，导致psts的endpoint无法自动删除的问题
2. 修复endpoint被误删后，nrs上IP地址无法回收，不能自动恢复的问题
3. 修复 psts 指定IP范围数组越界问题

#### 2.7.4
1. 修复双栈PSTS无法正常观测cep变更事件的问题
2. 更新CEP描述字段，增加IPs字段，展示cep所使用到的所有IP地址

#### 2.7.3
1. ENI支持重启后重新配置

#### 2.7.2 
1. 修复双栈集群中，IPv4和IPv6会形成地址数量差异的问题
2. 修复cipam插件响应地址中没有IPv6地址的问题
3. 修复NDP代理地址类型错误问题

#### 2.7.1
1. 修复容器网络v2与cilium存在node.status竞争关系的问题
2. 修复IPv6地址未在NetResourceSet中展示的问题

#### 2.7.0
1. 增加对EBC机型支持

### 2.6.0
1. 重构CCENode为NetResourceSet

### 2.5.0
1. 新增psts控制器，自动填充psts状态统计数据
2. 新增pstswebhook，填充psts默认值，并校验psts合法性
3. psts支持自动创建独占子网的自动创建和声明
4. 更换基础镜像
5. 指定from/to两种策略路由
6. 更新cptp插件的地址管理机制，去掉了位于主机veth上的IP地址
7. 更换单机ENI接口名和路由表生成算法，单机最大可支持252个ENI

### 2.4.0
1. 新增VPC Route模式，支持中心化的自动为节点分配IPv4/IPv6 CIDR
2. vpc eni模式增加源策略路由
3. vpc eni模式增加arp/ndp代理
4. cipam 在vpc route模式下增加默认路由解析，使用宿主机的默认路由作为容器的默认路由
5. 支持自定义单机最大ENI数和ENI最大IP数
6. bbc 支持辅助IP
7. VPC Route 支持动态路由地址分配
8. VPC Route 增加VPC状态同步和回收机制
9. cptp 支持双向静态邻居表配置

### 2.3.0
1. 新增支持BCC PSTS指定子网分配IP策略。
2. 支持IPv6固定IP
3. 升级BCE SDK，支持IPv6
4. 增加cptp CNI插件，支持IPv6固定邻居
5. 移除pcb-ipam，增加更为通用的cipam

### 2.2.0
新增支持外部扩展插件

### 2.1.0 
新增支持独占ENI