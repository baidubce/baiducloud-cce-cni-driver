# 策略案例
本文档介绍如何使用CCE Network Policy网络策略。
## 1. 前置说明
* 仅支持CCE标准独立集群和CCE托管集群，集群开启Network Policy后对集群网络性能有约小于 1%的性能影响。
* Network Policy网络策略依赖于网络插件实现，仅在集群开启eBPF增强时，才支持网络策略。网络策略插件的行为请参考社区文档。建议您充分理解、验证后启用网络策略。
* 使用 ebpf 网络策略，需要集群开启 eBPF 增强。保证内核版本在 5.10 以上。

## 2. L3 策略
第3层策略建立了关于哪些端点可以相互通信的基本连接规则，它通常工作于网络层 L3。可以使用以下集中方法定义第3层策略：
### 2.1 基于端点
基于端点的L3策略用于在CCE管理的ebpf 加速集群内的端点之间建立规则。基于端点的L3策略是通过在规则中使用端点选择器来定义的，以选择可以接收（在ingress）或发送（在egress）哪种流量。一个空的端点选择器允许所有流量。下面的例子更详细地说明了这一点。

#### 2.1.1 Ingress
如果存在至少一个Ingress规则，该规则使用端点选择器字段中的端点选择器选择目标端点，则允许端点从另一个端点接收流量。为了限制进入所选端点的流量，该规则使用`fromEndpoints`字段中的`endpointSelector`选择源端点。
##### 2.1.1.1 Ingress简短的允许 
以下示例说明了如何使用简单的入口规则来允许从标签`role=frontend`的端点到标签`role=backend`的端点进行通信。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "l3-rule"
spec:
  endpointSelector:
    matchLabels:
      role: backend
  ingress:
  - fromEndpoints:
    - matchLabels:
        role: frontend

```

##### 2.1.1.2 Ingress允许所有端点
一个空的端点选择器将选择所有端点，因此编写一条允许所有入口流量到达端点的规则可以按如下方式完成：
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "allow-all-to-victim"
spec:
  endpointSelector:
    matchLabels:
      role: victim
  ingress:
  - fromEndpoints:
    - {}
```

#### 2.1.2 Egress
如果至少存在一个出口规则，该规则使用端点选择器字段中的端点选择器选择目标端点，则允许端点向另一个端点发送流量。为了在出口到所选端点时限制流量，该规则使用toEndpoints字段中的端点选择器选择目标端点。

##### 2.1.2.1 Egress 简单允许
与 Ingress 规则相似，以下示例说明了如何使用简单的出口规则，允许从标签为role=frontend的端点到标签为role=backend的端点进行通信。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "l3-egress-rule"
spec:
  endpointSelector:
    matchLabels:
      role: frontend
  egress:
  - toEndpoints:
    - matchLabels:
        role: backend
```

> 一个空的端点选择器将根据`CiliumNetworkPolicy` 命名空间（默认情况下为`default`）从端点中选择所有出口端点。

#### 2.1.2 Ingress/Egress 黑名单策略
如果规则选择端点并包含相应的规则部分入口或出口，则可以在入口或出口处将端点置于黑名单拒绝模式。

> 此示例说明了如何将端点置于黑名单拒绝模式，而不会同时将其他对等端列入白名单。

```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "deny-all-egress"
spec:
  endpointSelector:
    matchLabels:
      role: restricted
  egress:
  - {}

```

### 2.2 基于Service
基于服务的规则规则仅在没有选择器的情况下对Kubernetes Service生效。
通过Egress规则中的`toServices`语句，可以允许从Pod到集群中运行的服务的流量。
当通过名称和命名空间或标签选择器定义时，支持没有`lableSelector`的Kubernetes服务。对于由Pod支持的服务，请在后端pod标签上使用基于端点的规则。

下面示例展示允许标签`id=app2`的所有端点与 `default` 命名空间下的 `myservice` 通信。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "service-rule"
spec:
  endpointSelector:
    matchLabels:
      id: app2
  egress:
  - toServices:
    - k8sService:
        serviceName: myservice
        namespace: default

```
也可以通过 labelSelecotor 选择允许的服务。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "service-labels-rule"
spec:
  endpointSelector:
    matchLabels:
      id: app2
  egress:
  - toServices:
    - k8sServiceSelector:
        selector:
          matchLabels:
            external: "yes"
```

#### Service 限制
`toServices`语句不得与`toPorts`语句组合在同一规则中。如果规则将这两个语句组合在一起，则策略将被拒绝。

### 2.3 基于特殊实体
`fromEntities` 用于描述可以访问所选端点的实体。`toEntities` 用于描述所选端点可以访问的实体。

目前 ebpf 策略支持的实体类型包括：

| 实体类型 | 描述                      |
| ---------------- | ----------------------------- |
| host | 主机实体包括本地主机。这也包括在本地主机上以主机网络模式运行的所有容器。 |
| remote-node | 除本地主机之外的任何连接集群中的任何节点。这也包括在远程节点上以主机网络模式运行的所有容器。 |
| kube-apiserver | kube-apiserver实体代表Kubernetes集群中的kube-apiserver。此实体表示kube-apiserver的两种部署：集群内和集群外。 |
| cluster | 集群是本地集群内所有网络端点的逻辑组。这包括本地集群的所有CCE托管端点、本地集群中的非托管端点，以及主机、远程节点和init标识。 |
| init | init实体包含引导阶段中尚未解析安全标识的所有端点。这通常只在非Kubernetes环境中观察到。 |
| health | 健康实体表示健康端点，用于检查集群连接健康状况。。Cilium管理的每个节点都承载一个健康端点。|
| unmanaged | 非托管实体表示不由Cilium管理的端点。非托管端点被视为集群的一部分，并包含在集群实体中。 |
| world | world 实体对应于集群外的所有端点。允许世界与允许CIDR 0.0.0.0/0相同。允许往返世界的另一种方法是定义基于细粒度DNS或CIDR的策略。 |
| all | all 实体表示集群所有端点。 |

#### 2.3.1 案例：限制访问 apiserver
允许标签为`env=dev`的端点访问kube-apiserver。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "dev-to-kube-apiserver"
spec:
  endpointSelector:
    matchLabels:
      env: dev
  egress:
    - toEntities:
      - kube-apiserver
```

#### 2.3.2 案例：访问集群中的所有节点
允许标签为`env=dev`的所有端点访问为特定端点提供服务的主机。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "to-dev-from-nodes-in-cluster"
spec:
  endpointSelector:
    matchLabels:
      env: dev
  ingress:
    - fromEntities:
      - host
      - remote-node
```

#### 2.3.3 案例：允许从集群外部访问
此示例显示了如何允许从集群外部访问标签为`role=public`的所有端点。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "from-world-to-role-public"
spec:
  endpointSelector:
    matchLabels:
      role: public
  ingress:
    - fromEntities:
      - world
```
### 2.4 基于IP/CIDR
CIDR策略用于定义与CCE不管理的端点之间的策略，因此没有与之关联的标签。这些通常是运行在特定子网中的外部服务、虚拟机或 裸金属机器。CIDR策略也可用于限制对外部服务的访问，例如限制对特定IP范围的外部访问。CIDR策略可以在入口或出口处应用。
CIDR规则将适用于连接一端为以下情况的流量：
* 群体外的网络端点。
* pod运行在主机网络命名空间。
* 集群内的节点IP。
相反，CIDR规则不适用于连接双方都由CCE管理或使用属于集群中节点（包括主机网络Pod）的IP的流量。如上所述，可以使用基于标签、服务或实体的策略来允许这种流量。
#### 2.4.1 Ingress
##### fromCIDR
允许与`endpointSelector`选择的所有端点通信的源CIDR列表。
##### fromCIDRSet
允许与`endpointSelector`选择的所有端点通信的源CIDR列表，以及每个CIDRs的CIDRs的可选列表，这些源CIDR是不允许通信的CIDR的子网。
#### 2.4.2 Egress
##### toCIDR
允许`endpointSelector`选择的端点与之通信的目标CIDR列表。请注意，由`fromEndpoints`选择的端点会自动回应到相应的目标端点。
##### toCIDRSet
允许与 `endpointSelector` 选择的所有端点通信的目标CIDR列表，以及每个源CIDR的CIDRs的可选列表，这些CIDR是不允许通信的目标CIDR的子网。

#### 2.4.3 案例：允许外部 CIDR 网段
此示例显示了如何允许标签为`app=myService`的所有端点与外部IP `20.1.1.1`以及CIDR `10.0.0.0/8` 通信，但不允许CIDR `10.96.0.0/12` 通信。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "cidr-rule"
spec:
  endpointSelector:
    matchLabels:
      app: myService
  egress:
  - toCIDR:
    - 20.1.1.1/32
  - toCIDRSet:
    - cidr: 10.0.0.0/8
      except:
      - 10.96.0.0/12
```
## 3. L4 层策略
### 3.1 限制Ingress/Egress
除了第3层策略之外，还可以单独指定4层策略。它限制了端点使用特定协议在特定端口上发送和或接收数据包的能力。如果没有为端点指定4层策略，则允许端点在所有4层端口和协议（包括ICMP）上发送和接收。如果指定了任何4层策略，则`ICMP`将被阻止，除非它与策略允许的连接有关。4层策略应用于服务端口。
可以使用`toPorts`字段在IngressEgress处指定4层策略。`toPorts`字段的结构`PortProtocol`结构，定义如下：

```golang
// PortProtocol 使用传输协议指定L4端口
type PortProtocol struct {
        // 端口可以是L4端口号，也可以是“http”或“http-8080”形式的名称。
        // 如果Port是命名端口，则忽略EndPort。
        Port string `json:"port"`

        // EndPort只能是L4端口号。当端口是命名端口时，它会被忽略。
        //
        // +optional
        EndPort int32 `json:"endPort,omitempty"`

        // 协议是L4协议。如果省略或为空，则任何协议都匹配。可接受的值：“TCP”、“UDP”、“/”ANY“
        //
        // 不支持ICMP上的匹配。
        //
        // +optional
        Protocol string `json:"protocol,omitempty"`
}

```

#### 3.1.1 案例：限制只有 80 端口可通信
以下规则将标签为`app=myService`的所有端点限制为只能在端口80上使用TCP向其它3层目标发送数据包：
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "l4-rule"
spec:
  endpointSelector:
    matchLabels:
      app: myService
  egress:
    - toPorts:
      - ports:
        - port: "80"
          protocol: TCP
```

#### 3.1.2 案例：端口范围
以下规则限制了标签为`app=myService`的所有端点只能在端口80-444上使用TCP向其它第3层目标发送数据包：
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "l4-port-range-rule"
spec:
  endpointSelector:
    matchLabels:
      app: myService
  egress:
    - toPorts:
      - ports:
        - port: "80"
          endPort: 444
          protocol: TCP
```

#### 3.1.3 案例：依赖标签的 L4 策略
此示例使标签`role=front`的所有端点能够与标签`role=backend`的所有端点通信，但它们必须在端口80上使用TCP进行通信。具有其他标签的端点将无法与具有标签`role=backend`的端点通信，具有标签`role=frontend`的端点将不能在80以外的端口上与`role=backend`通信。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "l4-rule"
spec:
  endpointSelector:
    matchLabels:
      role: backend
  ingress:
  - fromEndpoints:
    - matchLabels:
        role: frontend
    toPorts:
    - ports:
      - port: "80"
        protocol: TCP
```

#### 3.1.4 案例：依赖CIDR的 L4 策略
此示例允许标签为`role=crawler`的所有端点与CIDR `192.0.2.0/2`4内的所有远程目标通信，但它们必须在端口80上使用TCP进行通信。该策略不允许没有标签`role=crawler`的端点与CIDR `192.0.2.0/24`中的目标通信。此外，标签为`role=crawler`的端点将无法在端口80以外的端口上与CIDR `192.0.2.0/24`中的目标通信。
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "cidr-l4-rule"
spec:
  endpointSelector:
    matchLabels:
      role: crawler
  egress:
  - toCIDR:
    - 192.0.2.0/24
    toPorts:
    - ports:
      - port: "80"
        protocol: TCP
```

### 3.2 限制 ICMP/ICMP6
ICMP策略可以与第3层策略一起指定，也可以单独指定。它限制了端点在特定ICMP/ICMPv6类型上发送和或接收数据包的能力（支持类型（integer）和相应的CamelCase消息（string））。如果指定了任何ICMP策略，则4层和ICMP通信将被阻止，除非它与策略允许的连接有关。
ICMP策略可以使用`icmps`字段在入口和出口处指定。`icmps`字段采用`ICMPField`结构，定义如下：
```golang
// ICMPField is a ICMP field.
//
// +deepequal-gen=true
// +deepequal-gen:private-method=true
type ICMPField struct {
    // Family is a IP address version.
    // Currently, we support `IPv4` and `IPv6`.
    // `IPv4` is set as default.
    //
    // +kubebuilder:default=IPv4
    // +kubebuilder:validation:Optional
    // +kubebuilder:validation:Enum=IPv4;IPv6
    Family string `json:"family,omitempty"`

        // Type is a ICMP-type.
        // It should be an 8bit code (0-255), or it's CamelCase name (for example, "EchoReply").
        // Allowed ICMP types are:
        //     Ipv4: EchoReply | DestinationUnreachable | Redirect | Echo | EchoRequest |
        //                   RouterAdvertisement | RouterSelection | TimeExceeded | ParameterProblem |
        //                       Timestamp | TimestampReply | Photuris | ExtendedEcho Request | ExtendedEcho Reply
        //     Ipv6: DestinationUnreachable | PacketTooBig | TimeExceeded | ParameterProblem |
        //                       EchoRequest | EchoReply | MulticastListenerQuery| MulticastListenerReport |
        //                       MulticastListenerDone | RouterSolicitation | RouterAdvertisement | NeighborSolicitation |
        //                       NeighborAdvertisement | RedirectMessage | RouterRenumbering | ICMPNodeInformationQuery |
        //                       ICMPNodeInformationResponse | InverseNeighborDiscoverySolicitation | InverseNeighborDiscoveryAdvertisement |
        //                       HomeAgentAddressDiscoveryRequest | HomeAgentAddressDiscoveryReply | MobilePrefixSolicitation |
        //                       MobilePrefixAdvertisement | DuplicateAddressRequestCodeSuffix | DuplicateAddressConfirmationCodeSuffix |
        //                       ExtendedEchoRequest | ExtendedEchoReply
        //
        // +deepequal-gen=false
        // +kubebuilder:validation:XIntOrString
        // +kubebuilder:validation:Pattern="^([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5]|EchoReply|DestinationUnreachable|Redirect|Echo|RouterAdvertisement|RouterSelection|TimeExceeded|ParameterProblem|Timestamp|TimestampReply|Photuris|ExtendedEchoRequest|ExtendedEcho Reply|PacketTooBig|ParameterProblem|EchoRequest|MulticastListenerQuery|MulticastListenerReport|MulticastListenerDone|RouterSolicitation|RouterAdvertisement|NeighborSolicitation|NeighborAdvertisement|RedirectMessage|RouterRenumbering|ICMPNodeInformationQuery|ICMPNodeInformationResponse|InverseNeighborDiscoverySolicitation|InverseNeighborDiscoveryAdvertisement|HomeAgentAddressDiscoveryRequest|HomeAgentAddressDiscoveryReply|MobilePrefixSolicitation|MobilePrefixAdvertisement|DuplicateAddressRequestCodeSuffix|DuplicateAddressConfirmationCodeSuffix)$"
    Type *intstr.IntOrString `json:"type"`
}
```

#### 3.2.1 案例：ICMP/ICMPv6
以下规则限制了标签为`app=myService`的所有端点只能使用类型为8的ICMP和消息为`EchoRequest`的ICMPv6向任何3层目标发送数据包：
```yaml
apiVersion: "cilium.io/v2"
kind: CiliumNetworkPolicy
metadata:
  name: "icmp-rule"
spec:
  endpointSelector:
    matchLabels:
      app: myService
  egress:
  - icmps:
    - fields:
      - type: 8
        family: IPv4
      - type: EchoRequest
        family: IPv6
```