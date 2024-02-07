# VPC Route 网络
## 1 关键数据结构
### 1.1 NetResourceSet
```
apiVersion: cce.baidubce.com/v2
kind: NetResourceSet
metadata:
  creationTimestamp: "2023-04-26T07:58:42Z"
  finalizers:
  - RemoteRouteFinalizer
  name: 192.168.10.6
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: 192.168.10.6
    uid: e99f1dee-a7da-4d18-89e5-f2a9a5e75ecb
  resourceVersion: "26235476"
  uid: 289e9d25-6e49-490f-9e09-b88e9ee5c6c8
spec:
  addresses:
  - ip: 192.168.10.6
    type: InternalIP
  - ip: 172.10.4.88
    type: CCEInternalIP
  instance-id: i-ZdXYRWob
  ipam:
    podCIDRs:
    - 172.10.4.0/24
status:
  ipam:
    operator-status: {}
    pod-cidrs:
      172.10.4.0/24:
        status: in-use
    vpc-route-cidrs:
      172.10.4.0/24: in-use
```

* `RemoteRouteFinalizer`：由cce-network-operator 自动添加。当删除NetResourceSet对象时，会清理 `status.ipam.vpc-route-cidrs` 中的所有VPC路由选项。直到VPC路由清理完成，才允许删除NetResourceSet对象。
* `spec.address.CCEInternalIP`: cce内部IP，从`spec.ipam.podCIDRs`中分配，作为VPC路由模式下容器的路由IP。
* `spec.ipam.podCIDRs`: 由cce-network-operator为每个NetResourceSet分配的用于Pod容器IP的CIDR。每个节点可以有多个CIDR（只有一个CIDR耗尽，才会使用第二个CIDR）。
* `status.ipam.pod-cidrs`： 为节点分配的CIDR的状态。
  * released表示已被释放的CIDR（修改了cce-network-v2中指定Pod CIDR的参数，线上通常不会修改）。
  * depletedCIDR已满。
  * in-use正在使用（已给Pod分配IP）
*
* `status.ipam.vpc-router-cidrs`：PodCIDR和VPC路由表之间的同步状态。

### 1.2 CCEEndpoint
```
apiVersion: cce.baidubce.com/v2
  kind: CCEEndpoint
  metadata:
    name: deployment-example-85644859c9-h4mdg
    namespace: default
  spec:
    external-identifiers:
      container-id: 63e8df8d6fd846ac6ae0a5c2f34cfa71d0939ded7ca13059693290d33344c0b7
      k8s-namespace: default
      k8s-object-id: b1dd79b8-20b2-4a10-bec4-2a32d41f67f8
      k8s-pod-name: deployment-example-85644859c9-h4mdg
      netns: /proc/282117/ns/net
      pod-name: deployment-example-85644859c9-h4mdg
    network:
      ipAllocation:
        node: 172.22.10.4
        releaseStrategy: TTL
        type: Elastic
  status:
    external-identifiers:
      container-id: 63e8df8d6fd846ac6ae0a5c2f34cfa71d0939ded7ca13059693290d33344c0b7
      k8s-namespace: default
      k8s-object-id: b1dd79b8-20b2-4a10-bec4-2a32d41f67f8
      k8s-pod-name: deployment-example-85644859c9-h4mdg
      netns: /proc/282117/ns/net
      pod-name: deployment-example-85644859c9-h4mdg
    log:
    - code: ok
      state: ip-allocated
      timestamp: "2023-04-27T12:20:20Z"
    networking:
      addressing:
      - family: "4"
        ipv4: 192.168.1.148
      node: 172.22.10.4
    state: ip-allocated
```
CCEEndpoint 记录了网络端点的状态信息。

## 2 控制逻辑
### 2.1 节点就绪
只有所有CIDR成功写入VPC路由，节点才就绪。
如果CIDR未成功写入VPC路由，则cce-network-v2-agent Pod的健康检查会失败，并有以下日志：
```
level=error msg="fail to health check manager" error="cluster-pool-watcher's health plugin check failed:no VPCRouteCIDRs have been received yet" subsys=daemon-health
```

## 3 数据面
vpc 路由使用cptp网络驱动，它具备以下特征。
### 3.1 容器内
#### 3.1.1 容器地址
使用32/128位的地址，不归属任何子网。
```
# ip addr
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
2: eth0@if80494: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default 
    link/ether 06:cb:36:45:78:e1 brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 172.10.3.21/32 scope global eth0
       valid_lft forever preferred_lft forever
```

#### 3.1.2 容器路由
```
# ip route
default via 172.10.3.9 dev eth0 
172.10.3.9 dev eth0 scope link src 172.10.3.21 
172.10.3.21 via 172.10.3.9 dev eth0 src 172.10.3.21
```
值得注意的是网关地址是宿主机端veth的地址。这个地址是 cce-network-v2-agent 在Pod CIDR中申请的。该机器上所有的容器都使用这个gateway地址。且机器重启后，路由地址不变。

#### 3.1.3 邻居信息
容器内只有一条邻居信息，即默认网关的地址。mac地址是host端的veth的地址。
```
# ip neigh
172.10.3.9 dev eth0 lladdr 06:c5:90:3b:4d:6d PERMANENT
```

### 3.2 宿主机
#### 3.2.1 宿主机veth地址
宿主机上，每个veth的地址都是默认网关的地址
```
# ip addr
80494: vethda5db5af@if2: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default 
    link/ether 06:c5:90:3b:4d:6d brd ff:ff:ff:ff:ff:ff link-netnsid 10
    inet 172.10.3.9/32 scope global vethda5db5af
       valid_lft forever preferred_lft forever
    inet6 fe80::4c5:90ff:fe3b:4d6d/64 scope link 
       valid_lft forever preferred_lft forever
```
#### 3.2.2 路由
```
# ip r
default via 192.168.10.1 dev eth0 
169.254.0.0/16 dev eth0 scope link metric 1002 
169.254.30.0/28 dev docker0 proto kernel scope link src 169.254.30.1 
172.10.3.21 dev vethda5db5af scope host
```
宿主机上有一条到容器的路由，关联到host上的veth设备，scope为host。
#### 3.2.3 邻居表项
宿主机上有一条到容器的静态邻居表，关联到host上的veth设备，mac地址是容器内的mac地址。
```
# ip neigh
172.10.3.98 dev veth34408657 lladdr c6:05:b9:a3:bf:05 PERMANENT
```
