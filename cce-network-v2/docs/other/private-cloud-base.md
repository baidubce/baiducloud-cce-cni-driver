# 云底座IPAM部署文档
## 前置准备
### 节点label
安装ipam cni前，需要手动在边缘计算IPAM申请子网，并给k8s的node按照部署区域增加如下子网标签：

```
# kubectl label node/{node} topology.kubernetes.io/subnet={子网名}
kubectl label node/vm-13atpvfs-cn-taiyuan-un-f5bul topology.kubernetes.io/subnet=sbn-56
kubectl label node/vm-13atpvfs-cn-taiyuan-un-fpdpo topology.kubernetes.io/subnet=sbn-53
```

> 注意：集群新增节点后，不要忘记给节点增加 `topology.kubernetes.io/subnet={子网名}` 标签，否则节点无法创建容器网络的pod。

### cni 配置
云底座ipam不会安装macvlan等cni插件,也不会修改cni配置,使用云底座ipam 需要运维人员提前安装好cni插件,并配置好cni配置文件.推荐cni配置如下:
```
{
  "name":"macvlan-ipam",
  "cniVersion":"0.3.1",
  "plugins":[
    {
      "type":"macvlan",
      "mtu": 1500,
      "master":"eth0",
      "serviceCIDR": "10.1.104.0/22",
      // ipam 日志路径, 默认为空
      // "log-file": ""
      "ipam":{
        // 指定插件类型为 pcb-ipam
        "type":"pcb-ipam",
        // 下面的所有属性都仅用于测试使用
        // 是否开启cni调试模式 
        // "enable-debug": false,
        // 复制宿主机上的路由配置,该选项仅用于测试
        // "use-host-gate-way": false,
        // 从哪个网卡复制路由数据
        // "hostLink":"m0",
        // 手动配置路由
        // "routes": [
        //          {
        //            "dst": "0.0.0.0/0",
        //            "gw":"172.16.8.1",
        //          }
        //        ]
      }
    }
  ]
}

```
> `pcb-ipam` 插件不需要手动安装,云底座ipam agent在启动时会把 `pcb-ipam` 拷贝到 `/opt/cni/bin/pcb-ipam`路径下.

## 安装IPAM
### 配置
以下配置是默认的helm包配置，在新集群执行安装时，需要按需修改边缘IPAM等配置。

```
# Default values for helm.
# This is a YAML-formatted file.
# Declare variables to be passed into your templates.
CCEIPAMv2:
  operator:
    replicaCount: 1
    image:
      # operator镜像
      repository: registry.baidubce.com/cce-plugin-dev/cce_ipam_operator
      # 默认是 .Chart.AppVersion
      tag: ""
  agent:
    image:
      repository: registry.baidubce.com/cce-plugin-dev/cce_ipam_agent
      tag: ""

# cce守护进程配置
ccedConfig:
  debug: true
  # 在非k8s环境运行使用
  # k8s-kubeconfig-path: /run/kubeconfig
  k8s-client-burst: 10
  k8s-client-qps: 5

  # ipam 模式. privatecloudbase: 私有云底座，
  ipam: privatecloudbase
  # 启动ipv4
  enable-ipv4: true
  enable-ipv6: false
  cce-endpoint-gc-interval: 30s
  # cni 固定IP申请等待超时时间
  fixed-ip-allocate-timeout: 30s
  # 启用对远程固定IP的回收功能(开启后当系统发现远程记录有固定IP,但是k8s中没有与之对应的endpoint时,会删除远程固定IP)
  enable-remote-fixed-ip-gc: false
  # cni IP申请请求的超时时间
  ip-allocation-timeout: 30s
  # pod删除后固定ip保留时间
  fixed-ip-ttl-duration: 3650d
  
  # 私有云底座配置: 边缘IPAM服务地址
  bce-cloud-host: http://221.204.50.53:8007
  # 边缘IPAM region
  bce-cloud-region: cn-test-cm
```
### helm安装
#### 下载最新安装包代码库
```
# 下载安装包
git clone ssh://wangweiwei22@icode.baidu.com:8235/baidu/jpaas/cce-network-plugin baidu/jpaas/cce-network-plugin

cd cce-network-plugin/cce-ipam-v2
```

#### 安装IPAM
下载最新版 `private-cloud-base-ipam` helm包，并使用本文档中描述的配置覆盖默认配置，执行安装。
```
export KUBECONFIG={kubeconfig 绝对路径}
helm upgrade --install -n kube-system cce-ipam-v2 deploy/private-cloud-base-ipam -f {customer.yaml}
```

## 检查
### 检查节点ip缓存
```
# 查看ip缓存池列表
$: kubectl get cn
NAME                              AGE
vm-13atpvfs-cn-taiyuan-un-f5bul   37h
vm-13atpvfs-cn-taiyuan-un-fpdpo   37h

# 检查NetResourceSet是否成功分配了IP缓存
]# kubectl get cn vm-13atpvfs-cn-taiyuan-un-f5bul -oyaml
apiVersion: cce.baidubce.com/v2
kind: NetResourceSet
metadata:
  labels:
    kubernetes.io/hostname: vm-13atpvfs-cn-taiyuan-un-f5bul
    topology.kubernetes.io/subnet: sbn-56
  name: vm-13atpvfs-cn-taiyuan-un-f5bul
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: vm-13atpvfs-cn-taiyuan-un-f5bul
    uid: 1e381893-905f-435c-8acd-54934b117506
spec:
  addresses:
  - ip: 221.204.50.56
    type: InternalIP
  ipam:
    podCIDRs:
    - 100.64.1.0/24
    # 这里是所有已分配到该节点的IP列表
    pool:
      172.16.8.128: {}
      172.16.8.129: {}
      172.16.8.132: {}
      172.16.8.133: {}
      172.16.8.134: {}
      172.16.8.135: {}
      172.16.8.136: {}
      172.16.8.137: {}
      172.16.8.138: {}
      172.16.8.139: {}
      172.16.8.141: {}
      172.16.8.142: {}
    pre-allocate: 8
status:
  ipam:
    # 这里是所有已分配给pod的动态ip列表
    used:
      172.16.8.132:
        owner: kube-system/coredns-6d4b75cb6d-dkw8c
      172.16.8.134:
        owner: kube-system/coredns-6d4b75cb6d-zrpwf
      172.16.8.136:
        owner: test/dynamic-1-bf556cf4d-dnpjj
      172.16.8.139:
        owner: test/dynamic-1-bf556cf4d-kdb26
  privateCloudSubnet:
    Region: cn-test-cm
    cidr: 172.16.8.128/24
    enable: true
    gateway: 172.16.8.1
    ipVersio: 0
    name: sbn-56
    priority: 100
    purpose: public
    rangeEnd: 172.16.8.159
    rangeStart: 172.16.8.128
    vpc: vpc-test
```

### 检查固定IP分配
在创建pod前，给pod增加 cce.baidubce.com/fixedip=true标签可以使用固定IP的能力。
> 注意固定IP使用pod的namespace/name作为唯一标识，请务必确认pod删除重建后名字能保持一致，否则肯呢个导致集群IP泄露。例如使用apps/v1/StatefulSet工作负载创建的pod，pod重建后名字保持不变。而使用apps/v1/Deployment 创建的pod，删除重建后pod名字会发生变化。
 
```
# 查询所有固定ip列表
$: kubectl get cep -A -l cce.baidubce.com/fixedip=true
NAMESPACE   NAME        TYPE    RELEASE STRATEGY   ENDPOINT STATE   IPV4           IPV6
test        fixed-1-0   Fixed   TTL                restoring        172.16.8.206   
test        fixed-1-1   Fixed   TTL                restoring        172.16.8.143


# 检查固定IP分配历史详情
$: kubectl -n test get cep fixed-1-0 -oyaml
apiVersion: cce.baidubce.com/v2
kind: CCEEndpoint
metadata:
  labels:
    cce.baidubce.com/fixedip: "true"
    cce.baidubce.com/node: vm-13atpvfs-cn-taiyuan-un-fpdpo
    controller-revision-hash: fixed-1-5d4c6dbc67
    k8s-app: fixed-1
    statefulset.kubernetes.io/pod-name: fixed-1-0
  name: fixed-1-0
  namespace: test
spec:
  external-identifiers:
    k8s-namespace: test
    k8s-object-id: "2022-11-16T04:56:53Z"
    k8s-pod-name: fixed-1-0
    pod-name: fixed-1-0
  network:
    ipAllocation:
      node: vm-13atpvfs-cn-taiyuan-un-fpdpo
      releaseStrategy: TTL
      ttlSecondsAfterDeleted: 604800
      type: Fixed
status:
  # 固定IP绑定的pod信息
  external-identifiers:
    k8s-namespace: test
    # 这里使用pod的创建时间作为唯一标识
    k8s-object-id: "2022-11-16T04:56:53Z"
    k8s-pod-name: fixed-1-0
    pod-name: fixed-1-0
  # 固定IP pod的操作历史日志，最多保存10条  
  log:
  - code: ok
    state: IPAllocated
    timestamp: "2022-11-16T03:34:28Z"
  - code: ok
    state: restoring
    timestamp: "2022-11-16T04:56:01Z"
  - code: ok
    state: PodDeleted
    timestamp: "2022-11-16T04:56:52Z"
  - code: ok
    state: restoring
    timestamp: "2022-11-16T04:56:53Z"
  - code: ok
    state: IPAllocated
    timestamp: "2022-11-16T04:56:53Z"
  - code: ok
    state: restoring
    timestamp: "2022-11-16T04:57:01Z"
  - code: ok
    state: PodDeleted
    timestamp: "2022-11-16T04:57:34Z"
  - code: ok
    state: restoring
    timestamp: "2022-11-16T05:49:02Z"
  # 固定IP关联的亲和调度策略。 （首次创建固定IP pod不会有亲和策略，删除重建后亲和策略才生效）  
  matchExpressions:
  - key: topology.kubernetes.io/subnet
    operator: In
    values:
    - sbn-53
  # 固定IP的网络信息  
  networking:
    addressing:
    - cidrs4:
      - 172.16.8.128/24
      cidrs6:
      - ""
      gatewayV4: 172.16.8.1
      ipv4: 172.16.8.206
    node: vm-13atpvfs-cn-taiyuan-un-fpdpo
  state: restoring
```