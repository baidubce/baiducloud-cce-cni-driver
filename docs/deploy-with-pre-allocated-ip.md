# CCE CNI 使用预分配辅助 IP 模式

用户可以通过在主网卡预分配辅助 IP，然后使用 CCE CNI 构建容器网络。

在此模式下，BBC 和 BCC 机型均可加入集群作为 worker 节点，且用户无需提供 ak、sk 作为配置项提供给 CNI 组件。

## 使用条件
- 用户在节点加入集群前需要提前分配好主网卡辅助 IP；
- 节点在加入集群使用的生命周期内，如果想要删除辅助 IP，需要 drain 节点排空 Pod，否则可能引发节点上容器网络不通；
- 节点内需要能访问通 169.254.169.254 metadata api 地址；

## 如何部署

部署 CCE CNI 组件依赖 `helm chart` 渲染部署 yaml，用户可以自行修改 Helm Values 文件修改容器网络配置。

CCE CNI 为预分配辅助 IP 模式提供了一个默认的 Helm Values 文件，参考 [values-host-local-ip.yaml](../build/yamls/cce-cni-driver/values-host-local-ip.yaml)，其中配置指明了使用 `veth` 作为容器网卡类型。

为了使用更高性能的 `ipvlan` 作为容器网卡类型，用户可以替换 [values-host-local-ip.yaml](../build/yamls/cce-cni-driver/values-host-local-ip.yaml) 文件内容为:

```
CNIMode: host-local-secondary-ip-ipvlan
CCECNIImage: registry.baidubce.com/cce-plugin-pro/cce-cni:host-local
ServiceCIDR: # 集群 ClusterIP 网段, 例如 192.168.0.0/16
```

> **注意：** 使用 `ipvlan` 需要内核版本大于 4.9 且包含 `ipvlan` 模块。


在完成上述 Helm Values 文件的配置后，执行如下命令渲染出部署 yaml 进行检查:
```
make charts VALUES=build/yamls/cce-cni-driver/values-host-local-ip.yaml
```
或者渲染后后直接提交至 K8s 集群:
```
make charts VALUES=build/yamls/cce-cni-driver/values-host-local-ip.yaml | kubectl apply -f -
```