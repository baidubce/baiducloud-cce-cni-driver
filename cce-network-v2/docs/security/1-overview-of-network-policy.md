# 基于 ebpf 的网络策略概述
## 1. 背景
Kubernetes等容器管理系统部署了一个网络模型，为每个Pod（容器组）分配一个单独的IP地址。这确保了架构的简单性，避免了不必要的网络地址转换（NAT），并为每个单独的容器提供了全范围的端口号供使用。这种模型的逻辑结果是，根据集群的大小和Pod的总数，网络层必须管理大量的IP地址。

传统的安全实施架构基于IP地址过滤器。让我们来看一个简单的例子：如果所有标签为`role=frontend` 的Pod都应该被允许启动与所有标签为`role=backend`的Pod的连接，那么运行至少一个标签为`role=backend`的Pod的每个集群节点都必须安装一个相应的过滤器，允许所有`role=frontend` Pod的IP地址启动与所有本地`role=background` pod的IP地址的连接。应拒绝所有其他连接请求。这可能看起来像这样：如果目标地址是`10.1.1.2`，那么只有当源地址是以下地址之一时才允许连接`[10.1.2.2，10.1.2.3，20.4.9.1]`。

## 前置说明
* 仅支持CCE标准独立集群和CCE托管集群，集群开启Network Policy后对集群网络性能有约小于 1%的性能影响。
* Network Policy网络策略依赖于网络插件实现，仅在集群开启eBPF增强时，才支持网络策略。网络策略插件的行为请参考社区文档。建议您充分理解、验证后启用网络策略。
* 使用 ebpf 网络策略，需要集群开启 eBPF 增强。保证内核版本在 5.10 以上。

## 3. 推荐的控制措施
* 确保Kubernetes角色的范围正确地符合用户的要求，并且Pod的服务帐户权限严格地符合工作负载的需求。特别是，对敏感命名空间、执行操作和Kubernetes机密的访问都应该受到高度控制。
* 尽可能对工作负载使用资源限制，以减少拒绝服务攻击的机会。
* 确保只有在对工作负载的功能至关重要时才授予工作负载特权和功能，并确保有限制和监视工作负载行为的特定控制措施。
* 使用网络策略来确保Kubernetes中的网络流量是隔离的。
* 启用Kubernetes审计日志记录，将审计日志转发到集中式监控平台，并定义可疑活动的警报。
* 应定期修补容器镜像，以减少泄露的机会。

## 4. 网络策略的概念
### 4.1 策略执行模式
所有策略规则都基于白名单模型，也就是说，策略中的每个规则都允许与规则匹配的流量。如果存在两条规则，其中一条将匹配更广泛的流量，那么所有符合更广泛规则的流量都将被允许。如果两个或多个规则之间存在交集，则允许与这些规则的并集相匹配的流量。最后，如果流量不符合任何规则，则将根据策略执行模式将其丢弃。

默认情况下，所有端点都允许所有出口和入口流量。当网络策略选择端点时，它将转换为默认的拒绝状态，在该状态下只允许明确允许的流量。注意网络策略和流量方向相关：
* 如果任意规则选择了一个端点，并且该规则有一个入口部分，则该端点将进入默认的入口拒绝模式。
* 如果任意规则选择了一个端点，并且该规则有一个出口部分，则该端点将进入默认的出口拒绝模式。

这意味着端点开始时没有任何限制，第一个策略将切换端点的默认执行模式（按方向）。
也可以通过`EnableDefaultDeny` 字段启用黑名单模式的策略。例如，下面策略会拦截所有DNS流量，但不会阻止其他任何流量，即使它是应用于端点的第一个策略。管理员可以在整个集群范围内安全地应用此策略，而不会有将端点转换为默认拒绝并导致合法流量被丢弃的风险。
```
apiVersion: cilium.io/v2
kind: CiliumClusterwideNetworkPolicy
metadata:
  name: intercept-all-dns
spec:
  endpointSelector:
    matchExpressions:
      - key: "io.kubernetes.pod.namespace"
        operator: "NotIn"
        values:
        - "kube-system"
      - key: "k8s-app"
        operator: "NotIn"
        values:
        - kube-dns
  enableDefaultDeny:
    egress: false
    ingress: false
  egress:
    - toEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: kube-system
            k8s-app: kube-dns
      toPorts:
        - ports:
          - port: "53"
            protocol: TCP
          - port: "53"
            protocol: UDP
          rules:
            dns:
              - matchPattern: "*"
```

### 4.3 策略规则的数据结构
策略规则共享一个公共基类型，该基类型指定规则应用于哪些端点以及标识规则的公共元数据。每个规则都分为入口部分和出口部分。入口部分包含必须应用于进入端点的流量的规则，出口部分包含应用于来自与端点选择器匹配的端点的流量。可以提供入口、出口或两者兼而有之。如果入口和出口都被省略，则该规则无效。

```golang
type Rule struct {
        // 选择应用此规则的端点
        // 注意 EndpointSelector 和 NodeSelector 只能选择一个
        // +optional
        EndpointSelector EndpointSelector `json:"endpointSelector,omitempty"`

        // 选择应用此规则的 Node
        // 只能在 `CiliumClusterwideNetworkPolicies` 中使用
        //
        // +optional
        NodeSelector EndpointSelector `json:"nodeSelector,omitempty"`

        // Ingress 方向规则列表，即适用于进入端点的所有网络数据包。如果为空，则不使用入向规则。
        //
        // +optional
        Ingress []IngressRule `json:"ingress,omitempty"`

        // Egress 方向规则列表，即适用于离开端点的所有网络数据包。如果为空，则不使用出向规则。
        //
        // +optional
        Egress []EgressRule `json:"egress,omitempty"`

        // 可以定义需要写入meta store到策略身份的标签。
        // 通过kubernetes导入的策略规则会自动获得`io.cilium.k8s.Policy.name=name`标签，其中name对应于`NetworkPolicy`或`CiliumNetworkPolicy`资源中指定的名称。
        //
        // +optional
        Labels labels.LabelArray `json:"labels,omitempty"`

        // 描述策略的注释
        //
        // +optional
        Description string `json:"description,omitempty"`
}


```