## 组件名称
cce-network-eip-operator

## 组件介绍
CCE 容器网络 Pod 直通 EIP 扩展插件

## 组件功能
使用自定义资源 PodEIPBindStrategy 声明 EIP 资源，通过定义 Label Selector 为需要的 Pod 分配 EIP

使用示例：
```yaml
apiVersion: cce.baidubce.com/v2
kind: PodEIPBindStrategy
metadata:
  name: pebs-sample
  namespace: default
spec:
  selector:
    matchLabels:
      app: nginx
  staticEIPPool:
    - 106.13.198.78
    - 182.61.25.1
```

## 使用场景
需要 Pod 直通 EIP 的高性能业务场景

## 部署情况

## 版本记录
| 版本号 | 适配集群版本 | 更新时间   | 更新内容                                                                                                         | 影响 |
| ------ | ------------ | ---------- | ---------------------------------------------------------------------------------------------------------------- | ---- |
| 0.1.0  | CCE/v1.20+   | 2023.05.31 | 首次上线                                                                                                         | -    |