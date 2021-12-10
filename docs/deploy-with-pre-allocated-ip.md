# 百度云自建集群如何使用 CCE CNI

用户可以通过在主网卡预分配辅助 IP， 然后使用 CCE CNI 建容器网络。 在此模式下，BBC 和 BCC 机型均可加入集群作为 worker 节点，且用户无需提供 ak、sk 作为配置项提供给 CNI 组件。

## 使用条件
- 用户在节点加入集群前需要分配好辅助 IP；
- 节点在加入集群使用的生命周期内，不允许删除辅助 IP；
- 节点内需要能访问通 169.254.169.254 matadata api 地址；

## 如何部署

用户使用如下 Helm Values 文件:

```
CNIMode: host-local-secondary-ip-veth
CCECNIImage: registry.baidubce.com/cce-plugin-dev/cce-cni:host-local
```

执行如下命令渲染出部署 yaml 进行检查:
```
make charts VALUES=build/yamls/cce-cni-driver/values-host-local-ip.yaml
```
或者渲染后后直接提交至 K8s 集群:
```
make charts VALUES=build/yamls/cce-cni-driver/values-host-local-ip.yaml | kubectl apply -f -
```