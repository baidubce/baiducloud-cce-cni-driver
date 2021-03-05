# cce-eni-ipam

中心化 ipam-server，负责申请/释放 ENI 辅助 IP。与 cni 插件 eni-ipam 通过 HTTP 协议通信，将申请/释放 IP 的结果传递给 cni。

## sts 固定 IP

对于需要固定 IP 的 sts，在 sts pod 的 annotations 中添加 `cce.io/sts-enable-fix-ip: "True"`。

样例如下：
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: foo
spec:
  serviceName: foo
  replicas: 3
  selector:
    matchLabels:
      app: foo
  template:
    metadata:
      labels:
        app: foo
      annotations:
        cce.io/sts-enable-fix-ip: "True"
    spec:
      containers:
      - name: foo
        image: nginx
```

sts 固定 IP 的用法：
1. 创建/扩容 sts 会从子网中随机选择 IP；
2. 删除 sts Pod， 重建的 Pod IP 会固定；
3. *缩容/删除 sts，会导致 sts Pod IP 被回收掉；如果再扩容/重建，新建的 Pod 无法拥有之前的 IP*


## sts 固定 IP 永久保留

由于某些场景下希望：sts 在缩容/删除操作之后，扩容/重建仍能够让 Pod 保持固定 IP。
这就需要在 sts pod 的 annotations 中添加 `cce.io/sts-pod-fix-ip-delete-policy: "Never"`。

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: foo
spec:
  serviceName: foo
  replicas: 3
  selector:
    matchLabels:
      app: foo
  template:
    metadata:
      labels:
        app: foo
      annotations:
        cce.io/sts-enable-fix-ip: "True"
        cce.io/sts-pod-fix-ip-delete-policy: "Never"
    spec:
      containers:
      - name: foo
        image: nginx
```