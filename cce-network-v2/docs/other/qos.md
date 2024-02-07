# 容器网络 QoS
CCE 提供了两种对容器网络进行 QoS 管理的方式：带宽管理和出口数据包优先级管理。通过在 Pod 上设置 annnotation，可以控制容器网络 QoS。

## 1. 带宽管理
带宽管理配置如下：

| annotation | 描述 | 示例 |
| --- | --- | --- |
| `kubernetes.io/ingress-bandwidth` | 容器 ingress 带宽 | 10M |
| `kubernetes.io/egress-bandwidth` | 容器 egress 带宽 | 10M |

### 1.1 带宽限制的单位
带宽管理支持的单位有：

* `B`: 字节
* `K`/`KB`/`KIB`：千字节
* `M`/`MB`/`MIB`：兆字节
* `G`/`GB`/`GIB`：吉字节
* `T`/`TB`/`TIB`：太字节

### 1.2 带宽管理原理
CCE 容器网络当前仅支持使用 tc 实现带宽管理，通过 tc 配置容器网卡的 ingress 和 egress 的 tbf qdisc，实现容器网络 ingress 和 egress 的限速。
关于 [tbf可以在 linux man 手册中查看](https://man7.org/linux/man-pages/man8/tc-tbf.8.html) 。

## 2. 出口数据包优先级管理
出口数据包优先级管理配置如下：
| annotation | 描述 | 示例 |
| --- | --- | --- |
| `cce.baidubce.com/egress-priority` | 容器 egress 数据包优先级 | Burstable |

### 2.1 出口数据包优先级取值
* `Guaranteed`: 最高优先级，用于需要极低延迟的场景
* `BestEffort`: 普通优先级，用于需要高吞吐的场景
* `Burstable`: 低优先级，用于延迟不敏感的场景

### 2.2 出口数据包优先级管理原理
CCE 容器网络当前仅支持使用 tc 实现出口数据包优先级管理，例如在 VPC-ENI 场景下，如果设置了数据包出口优先级，则 ENI 上的 tc 示例配置如下：
cce-eni-0
- qdisc mq 1: dev cce-eni-0 root 
-   qdisc prio 8001: dev cce-eni-0 parent 1:2 bands 3 priomap 1 2 2 2 1 2 0 0 1 1 1 1 1 1 1 1
-       filter protocol ip pref 1 u32 chain 0 fh 800::800 order 2048 key ht 800 bkt 0 terminal flowid ??? not_in_hw  match ac16161e/ffffffff at 12
-   qdisc prio 8002: dev cce-eni-0 parent 1:1 bands 3 priomap 1 2 2 2 1 2 0 0 1 1 1 1 1 1 1 1
-       filter protocol ip pref 1 u32 chain 0 fh 800::800 order 2048 key ht 800 bkt 0 terminal flowid ??? not_in_hw  match ac16161e/ffffffff at 12

通过为 ENI 网卡设置 mq qdisc，再为每个 mq 队列设置 prio qdisc，再为每个 prio 队列设置 filter，实现容器网络出口数据包优先级管理。

## 使用限制
1. annotation 需要在创建 Pod 时配置，不支持动态修改。
2. 网络 QoS 需要配合 cce-network-v2 2.9.0 及以上版本使用。
3. QoS 当前仅支持 linux 操作系统，且要求内核版本 3.10 以上。