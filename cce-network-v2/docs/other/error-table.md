  
# 概述
本文档适用于CCE容器网络常见问题排查，文档解释了 CCE 容器网络组件中出现的所有的事件编码并给出解决方案。

# 1. Pod 错误事件
Pod 错误事件主要描述 Pod 无法正常启动的情况。Pod 的网络错误事件信息见于 Pod 对象上，可以通过 `kubectl describe pod {podName}` 查询到所有 Pod 相关的事件。
## 1.1 CNI 错误
CNI 错误指的是在单机创建Pod时，容器网络组件无法正常创建 Pod 的网络资源。CCE 对创建 Pod 失败的原因进行了分类，并给出了解决方案。

### 1.1.1 VPC-ENI 模式
| 错误代码 | 解释  | 触发条件 | 解决方案 |
| --- | --- | --- | --- |
| ENIIPCapacityExceed | ENI 上挂载的辅助 IP 数量达到配额限制 |  容器网络组件创建的 ENI 上辅助 IP 数量超过配额限制。<br/> 点击查看[ENI 配额](https://cloud.baidu.com/doc/VPC/s/9jwvytur6#%E5%BC%B9%E6%80%A7%E7%BD%91%E5%8D%A1%E9%85%8D%E9%A2%9D) | 新增节点，并将 Pod 调度到新节点。 |
| ENICapacityExceed | ENI 数量达到配额限制 |  节点上挂载的 ENI 已达到配额，且 ENI 已绑定的所有辅助 IP 已经达到配额限制。<br/> 点击查看[ENI 配额](https://cloud.baidu.com/doc/VPC/s/9jwvytur6#%E5%BC%B9%E6%80%A7%E7%BD%91%E5%8D%A1%E9%85%8D%E9%A2%9D) | 新增节点，并将 Pod 调度到新节点。 |
| WaitCreateMoreENI | 等待创建新的 ENI | 节点上所有已经挂载的 ENI 的辅助 IP 耗尽，正在等待创建新的 ENI | 等待kubelet自动重试创建 Pod 网络资源 |
| IPPoolExhausted | 缓存池中的 IP 耗尽 | 容器网络预申请的 IP 地址池中的 IP 全部分配完成，正在等待容器网络组件预申请新的 IP | 等待kubelet自动重试创建 Pod 网络资源 |
| NoAvailableSubnet | 节点所在的可用区没有可用的子网 | 节点所在的可用区没有可用的子网，节点无法创建新 ENI | 新建与节点同可用区的子网，并[添加新子网到 CCE 集群](https://cloud.baidu.com/doc/CCE/s/Jkqw0e1my)  |
| SubnetNoMoreIP |  ENI 已绑定的子网 IP 耗尽 | ENI 已绑定的子网 IP 耗尽，无法为 ENI 绑定新的辅助IP。 | **方案1.** 新建与节点同可用区更大区间的子网，并[添加新子网到 CCE 集群](https://cloud.baidu.com/doc/CCE/s/Jkqw0e1my).然后删除当前节点，并购买新的同可用区节点加入到 CCE 集群。 <br/> **方案2.** 使用[指定子网分配 IP](https://cloud.baidu.com/doc/CCE/s/Zlpuw49hx) 。|
| OpenAPIError | 访问 open API 遇到错误 | 访问 open API 遇到了偶发问题，如访问超时等。 | CCE 会自动重试，如长时间未恢复，请联系客服 |
| failed to set bandwidth | 容器网络组件无法为 ENI 设置带宽管理 | 容器网络组件无法为 Pod 设置带宽，如 内核版本过低等。 | 联系客服解决 |

### 1.1.2 VPC-Route 模式
| 错误代码 | 解释  | 触发条件 | 解决方案 |
| --- | --- | --- | --- |
| NoMoreIP |  节点没有可用 IP | 节点上所有可用 IP 都分配完毕，无法为 Pod  分配新的 IP。 | [VPC-Route 扩容容器网段](https://cloud.baidu.com/doc/CCE/s/Akbbu4a21)|

# 2. 节点错误事件
CCE 容器网络中与节点相关的网络事件主要包含两类，Node 和网络资源错误事件。
## 2.1 Node 错误事件
Node 错误事件主要描述节点无法正常启动的情况。Node 的网络错误事件信息见于k8s 的 Node 对象上，可以通过 `kubectl describe node {nodeName}` 查询到所有 Node 相关的事件。


| 错误代码 | 解释  | 触发条件 | 解决方案 |
| --- | --- | --- | --- |
| MetaAPIError01 | 容器网络组件无法通过 MetaAPI 获取机器实例 | 在不受支持的机型上部署了 CCE 容器网络组件 | 联系客服解决 |
| MetaAPIError02 | 容器网络组件无法通过 MetaAPI 获取机器地域等元数据信息 | 在不受支持的机型上部署了 CCE 容器网络组件 | 联系客服解决 |

## 2.2 网络资源错误事件
网络资源错误事件主要用于描述节点无法完成初始化，或者无法完成 ENI 等资源准备的情况。NetworkResourceSet 是 CCE 定义的网络资源管理对象，可以通过  `kubectl describe nrs {nodeName}` 查询到节点相关的所有网络相关事件.

| 错误代码 | 解释  | 触发条件 | 解决方案 |
| --- | --- | --- | --- |
| VPCQuotaLimitExceeded |failed to create vpc route rule 由于VPC 资源配额受限，无法创建更多 VPC 路由规则。 点击查看[VPC 配额](https://cloud.baidu.com/doc/VPC/s/9jwvytur6#路由表配额)| VPC 路由规则达到上限 | 在配额中心提出申请，联系客服解决 |
| CreateRouteRuleFailed | 容器网络组件无法通过 open API 创建路由 | 访问 open API 遇到了偶发问题 |  CCE 会在 2 分钟内自动恢复，如长时间未恢复，请联系客服 |