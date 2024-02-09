# 容器网络组件小版本升级 SOP
本操作仅适用于使用v1 版本容器网络的CCE集群，请先确认容器网络组件版本号是否为1.x。
注意：本 SOP 仅适用于小版本升级。例如从1.8.3 -> 1.8.4。不适用从例如 1.8.3 -> 1.9.0的大版本升级。

> cce 容器网络插件版本号命名规则：
1. 版本号通常采用 a.b.c 三位命名格式
2. 第一位版本号 a 的升级意为着组件替换，是不能前向兼容升级。例如 v1 升级 v2，暂时不支持升级（有计划），升级时需要考虑数据迁移、禁止变更、机器重建等复杂的升级机制。
3. 第二位版本号 b 的升级意为着新的特性或功能，有可能引入不兼容的升级。例如 2.8.0 -> 2.9.0，在升级时一定要通过对应版本的 helm 包做升级。
4. 第三位版本号 c 的升级意为着 bug 修复或者质量优化，不会引入新特性，一定会考虑前向兼容。例如 2.8.3 -> 2.8.4，可以直接替换镜像版本号做升级。

## 1.1 影响范围
SOP的影响范围明确如下：
1. 无影响
SOP经过验证，不会影响正在运行的业务。
# 2. 操作步骤
## 2.1 基础环境确认
在执行升级之前，需要确认以下信息：
1. 集群是否使用 v1 容器网络

### 2.1.1 确认是否使用 v1 容器网络
检查集群中是否有 `cce-cni-node-agent` 对象，如果有该对象，则使用的容器网络版本是 v1.x 集群。

```
$ kubectl -n kube-system get cm cce-cni-node-agent
NAME                 DATA   AGE
cce-cni-node-agent   1      125d
```

## 2.2 修改`cce-cni-node-agent`的镜像版本
### 2.2.1 修改镜像版本
`cce-cni-node-agent` 有 `cce-cni-node-agent` 和 `install-cni-binary` 两个容器，使用相同的镜像，更新时切记两个容器的版本都要更新。
```
$ kubectl -n kube-system set image daemonset/cce-cni-node-agent cce-cni-node-agent=registry.baidubce.com/cce-plugin-pro/cce-cni:v1.9.3 install-cni-binary=registry.baidubce.com/cce-plugin-pro/cce-cni:v1.9.3
daemonset.apps/cce-cni-node-agent image updated
```

上面的命令中 registry.baidubce.com/cce-plugin-pro/cce-cni:v1.9.3代表新版本的镜像地址，在按本文档操作更新时，请按需选择新的镜像版本。
* registry.baidubce.com/cce-plugin-pro/cce-cni：cce 正式版本镜像地址。
* `cce-cni-node-agent=registry.baidubce.com/cce-plugin-pro/cce-cni:v1.9.3`： 设置容器网络单机代理工作容器的镜像。
* `install-cni-binary=registry.baidubce.com/cce-plugin-pro/cce-cni:v1.9.3`：设置 cni 插件容器镜像，与容器网络单机代理工作容器镜像保持一致。
### 2.2.2 验证
所有的 agent Pod 都进入就绪状态，且 UP-TO-DATE 的实例数量等于DESIRED的实例数，更新就完成了。

```
$ kubectl -n kube-system get ds cce-cni-node-agent
NAME                DESIRED   CURRENT   READY   UP-TO-DATE   AVAILABLE   NODE SELECTOR   AGE
cce-cni-node-agent   2         2         2       2            2           <none>          12d
```

* DESIRED： 期望的实例数
* UP-TO-DATE: 已更新的实例数

## 2.3 修改 `cce-eni-ipam` 的版本
### 2.3.1 修改镜像版本
`cce-eni-ipam` 只有 `cce-eni-ipam` 一个容器，与 `cni-node-agent` 使用相同的镜像，下面是更新命令

```
$ kubectl -n kube-system set image deployment/cce-eni-ipam cce-eni-ipam=registry.baidubce.com/cce-plugin-pro/cce-cni:v1.9.3
deployment.apps/cce-eni-ipam image updated
```

* `cce-eni-ipam=registry.baidubce.com/cce-plugin-pro/cce-cni:v1.9.3`：设置 `cce-eni-ipam` 的镜像版本


### 2.3.2 验证
所有的 cce-eni-ipam Pod 都进入就绪状态，且 UP-TO-DATE 的实例数量等于DESIRED的实例数，更新就完成了。

```
$ kubectl -n kube-system get deploy cce-eni-ipam 
NAME                   READY   UP-TO-DATE   AVAILABLE   AGE
cce-eni-ipam   2/2     2            2           12d
```

* DESIRED： 期望的实例数
* UP-TO-DATE: 已更新的实例数

#### 2.3.2.1 `cce-eni-ipam` Pod 长时间无法就绪
由于 `cce-eni-ipam` 会优先调度到 master 机器上，如遇见由端口冲突导致新的 `cce-eni-ipam` Pod 无法调度的情况，可以尝试先缩容再扩容，最后再检查所有 Pod 是否全部 Ready。

```
# 1. 执行缩容
$ kubectl -n kube-system scale deploy cce-eni-ipam --replicas=0
deployment.apps/cce-eni-ipam scaled

# 2. 接着执行下面命令扩容
$ kubectl -n kube-system scale deploy cce-eni-ipam --replicas=3

# 3. 等待所有 Pod 更新并就绪
kubectl -n kube-system get deploy cce-eni-ipam
NAME                   READY   UP-TO-DATE   AVAILABLE   AGE
cce-eni-ipam   2/2     2            2           12d
```

# 3. 回滚
如遇见升级不符合预期，或其他异常情况，请及时操作回滚。回滚方案如下：
```
# 1. 撤销对 cce-eni-ipam 最近的变更
$ kubectl -n kube-system rollout undo deploy/cce-eni-ipam
# 2. 撤销对cni-node-agent的最近变更
kubectl -n kube-system rollout undo daemonset/cce-cni-node-agent
```

## 3.1 指定版本回滚
如果在升级过程中，对网路哦插件部署进行过多次修改，例如除更换镜像外还修改 cce-eni-ipam 的副本数，每次修改动作都会会产生一个新的 revision。所以在确定回滚之前，建议**检查每个 revision 对应的内容**。

```
# 1. 列出 ipam 的历史版本
$ kubectl -n kube-system rollout history deploy/cce-eni-ipam 
deployment.apps/cce-eni-ipam 
REVISION  CHANGE-CAUSE
1         <none>
2         <none>
3         <none>

# 2. 查看 revision 为 2 的历史部署记录详情
$ kubectl -n kube-system rollout history deploy/cce-eni-ipam --revision=2

# 3. 确认 revision 无误后，执行回滚到 revision 2
$ kubectl -n kube-system rollout undo deploy/cce-eni-ipam --to-revision=2
```