# 容器内 sysctl 配置

## 背景

部分容器化应用需要对容器网络命名空间的 sysctl 参数进行优化，尽管可以使用带有特权的 initContainer 实现初始化配置，但更推荐的方式是利用 CNI 原生支持。

CCE CNI 提供了 sysctl 插件，基于 [CNI Chain](https://github.com/containernetworking/cni/blob/master/SPEC.md#overview-1) 能够进行灵活的配置。

本文描述了如何使用和配置 CCE CNI，从而实现集群粒度和应用粒度的容器 sysctl 配置。

## 使用说明

### 1 确认容器网络模式

执行 `kubectl get cm -n kube-system cce-cni-node-agent -o yaml`，查看 `cniMode` 字段。

- `vpc-route-` 开头表示基于 VPC 实例路由的网络方案
- `vpc-secondary-ip-`, 开头表示基于弹性网卡的网络方案

### 2 修改 CNI 配置文件模板

根据步骤 1 中获取的网络模式，执行 `kubectl edit cm -n kube-system cce-cni-config-template`, 修改对应模式的 CNI 配置文模板。

手动编辑 `plugins` 列表, 在最后加上 sysctl 的配置文件，等待 1 分钟后所有节点的 CNI 配置将会更新。

修改完的样例如下:

```yaml
  cce-cni-secondary-ip-veth: |
    {
        "name":"{{ .NetworkName }}",
        "cniVersion":"0.3.1",
        "plugins":[
            {
                "type":"ptp",
                "enableARPProxy":true,
                "vethPrefix":"veth",
                "mtu": {{ .VethMTU }},
                "ipam":{
                    "type":"eni-ipam",
                    "endpoint":"{{ .IPAMEndPoint }}",
                    "instanceType":"{{ .InstanceType }}",
                    "deleteENIScopeLinkRoute":true
                }
            },
            {
              "type":"sysctl",
              "kubeconfig":"/etc/cni/net.d/cce-cni.d/cce-cni.kubeconfig"
            }
        ]
    }
```

### 3 集群粒度的配置（可选）

在步骤 2 中的配置里，可以继续添加 `sysctl` 字段，指明需要配置的参数。

如下的配置生效后，集群内所有新建的容器都会将 `/proc/sys/net/core/somaxconn` 设置为 500。
```json
{
  "type":"sysctl",
  "kubeconfig":"/etc/cni/net.d/cce-cni.d/cce-cni.kubeconfig",
  "sysctl":{
    "net.core.somaxconn":"8192"
  }
}
```

### 4 应用粒度的配置（可选）

在完成步骤 2 后，用户可以通过指定 Pod 的 annotations，传递 sysctl 参数的配置。

采用如下的 yaml 的创建的 nginx 容器，将会设置 `/proc/sys/net/core/somaxconn` 为 8192 以及 `/proc/sys/net/ipv4/tcp_tw_reuse` 为 1。

```yaml
kind: Deployment
apiVersion: apps/v1
metadata:
  name: nginx-example
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx-example
  template:
    metadata:
      labels:
        app: nginx-example
      annotations:
        net.sysctl.cce.io/net.core.somaxconn: "8192"
        net.sysctl.cce.io/net.ipv4.tcp_tw_reuse: "1"
    spec:
      containers:
        - name: nginx-example
          image: nginx:alpine
          imagePullPolicy: IfNotPresent
```


## 注意事项

- 确保 CNI 配置是一个合法的 JSON 格式文件
- 不同版本内核中 netns 可配置的 sysctl 参数不同，请小心配置否则容器可能无法启动


## 参考文档

- [Documentation for /proc/sys/net/*](https://www.kernel.org/doc/Documentation/sysctl/net.txt)
- [/proc/sys/net/ipv4/* Variables](https://www.kernel.org/doc/Documentation/networking/ip-sysctl.txt)
- [Kernel doc for networking](https://www.kernel.org/doc/Documentation/networking/)