# 用户自定义 CNI 插件方法
cce-network-v2 组件提供了用户自定义 CNI 插件的方法，用户可以自行编写 CNI 插件，然后通过配置文件进行部署。同时，cce-network-v2 组件也提供了默认的 CNI 插件，用户也可以通过配置文件来选择适当的配置文件。

## 1. 前提条件
- 已安装 cce-network-v2 组件，版本不小于 2.10.0。

## 2. 自定义 CCE 官方支持的插件
下面提供 CCE 官方支持的插件列表，用户可以通过安装 cce-network-v2 时通过 `ccedConfig.ext-cni-plugins` 配置文件进行选择。
| 插件名称 | 提供者 | 插件描述 | 插件版本 |
| --- | --- | --- | --- |
| portmap | 社区 | 社区的 CNI 插件，支持Pod 上配置端口直通能力。当开启 ebpf 加速时，该插件会自动失效。 | v1.0.0 及以上版本 |
| cilium-cni | CCE | Cilium CNI 插件，支持网络策略、service加速等。 | v1.12.5-baidu 及以上版本 |
| endpoint-probe | CCE 提供的 CNI 插件，用于支持 Pod Qos 等能力。 | v2.9.0 及以上版本 |
| cptp | CCE | CCE 提供的默认 CNI 插件，支持Pod基础网络通信能力。 | v1.0.0 及以上版本 |
| exclusive-device | CCE | CCE 提供的 CNI 插件，支持Pod独占网卡能力。 | v2.6.0 及以上版本 |
| sbr-eip | CCE | CCE 提供的 CNI 插件，支持Pod 直通 EIP 功能。 | v2.6.0 及以上版本 |

例如需要开启 portmap 插件，则可以通过如下配置文件进行重新部署。
```yaml
ccedConfig:
    ext-cni-plugins:
    - portmap
    - endpoint-probe

```

## 3. 自定义 CNI 插件
CCE 也支持保留用户自定义的 CNI 插件，用户可以通过修改主机配置文件 `/etc/cni/net.d/00-cce-cni.conflist` 自行进行部署CNI 配置文件。CCE 在更新时，会使用用户自定义的 CNI 插件。
```json
{
    "name":"generic-veth",
    "cniVersion":"0.4.0",
    "plugins":[
        {
            "type":"cptp",
            "ipam":{
                "type":"cipam",
            },
            "mtu": 1500
        }
        ,{
            "type": "endpoint-probe"
        },
        {
            "type": "user-cni"
        }
    ]
}
```

CCE 容器网络在启动时会自动读取该配置文件，如果发现了用户的自定义 CNI 插件，则在更新 CNI 配置时，会把用户自定义 CNI 插件自动插入到插件链的尾端。

### 3.1 使用限制
用户自定义的 CNI 插件，必须满足以下条件：
- 确保 CNI 插件使用的规范版本 >= 0.4.0。
- 确保 CNI 插件的 `type` 字段不为空且不要与已有插件重复。
- 确保 CNI 插件的可执行程序`/opt/cni/bin/{user-cni}` 存在且可执行，否则 kubelet 无法正常创建 Pod。
- 确保 CNI 插件完整的遵循了 CNI 规范，确保 CNI DEL 操作可以正常执行。防止 CNI 插件异常导致 Pod 无法删除重建，持续在故障中无法恢复。 
