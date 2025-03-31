---
title: exclusive-rdma agent
description: "cmd/exclusive-rdma-agent/README.md"
---

# exclusive-rdma-agent
cce 定制插件 exclusive-rdma 对应的 agent，协助完成功能：
1. pod 独占 RDMA 网卡

## 概述
本 agent 需要通过镜像以 daemonset 的形式部署在集群中，使用 hostNetwork 模式运行在 RDMA 节点上。
完成将 cni 插件：exclusive-rdma 安装到 RDMA 节点上并监控 cni 插件的配置文件功能，当发生修改时维持本插件的配置存在。
监听unix http socket，提供接口给 cce 插件 exclusive-rdma 来完成其委托访问集群。

## 配置示例
见本目录下的 excluive-rdma-agent.yaml
注意需要 serviceAccount 授权，以及使用插件的必要启动参数。（均已在 yaml 中给出）

```yaml
spec:
  template:
    spec:
      containers:
        args:
            - --config=/etc/cni/net.d/00-cce-cni.conflist 
            - --rstype=rdma 
            - --driver=mlx5_core 
```

其中：
config： 插件配置文件路径以及文件名
rsType： 有device plugin或其他提供的 rdma 资源标识，当 pod 有独占 rdma 网卡需求时，会用到这个标识。
        （在其spec.containers.resources.requests与spec.containers.resources.limits字段内）
driver： rdma 网卡驱动类型，例如目前的 mlx5_core