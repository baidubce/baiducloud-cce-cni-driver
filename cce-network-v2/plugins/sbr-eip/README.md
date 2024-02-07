sbr-eip 为容器设置eip指定源路由。

## Overview

该插件时专用插件，用于支持百度智能云EIP直通模式。插件的效果是，把EIP变为容器内可见，并为EIP设置规则和路由。

### 应用场景
在一些使用网络分离进行流量管理和安全的应用程序中，系统没有办法事先判断应该使用哪个接口，但应用程序能够做出决定。
例如实时通信场景的应用程序可能有两个网络，一个是管理网络，另一个是SIP（电话）流量网络，其规则规定：
* SIP流量（仅限）必须通过SIP网络进行路由；
* 所有其他业务（但没有SIP业务）必须通过管理网络进行路由。
这种场景下无法根据目的地IP进行配置，因为基础平台无法判断互联网上的目的地IP是用于下载更新软件包的地址还是远程SIP端点的地址。
所以我们提供了基于源的路由方式：
* 应用程序在接口上显式侦听传入流量。
* 当应用程序希望通过SIP网络发送到某个地址时，它会显式绑定到该网络上设备的IP。
* SIP接口的路由在单独的路由表中配置，并且规则被配置为基于源IP地址使用该表。

容器默认提供了一个私有地址，这个私有地址可以作为管理IP。而EIP可以作为指定源地址的路由，这样应用可以把SIP相关的通信都交给EIP，并使用专属的路由。

### 输入
sbr-eip 插件会调用cce-network-v2-agent /v1/endpoint/extplugin/status 接口，尝试查询已经就绪的外部插件扩展数据。如果EIP没有没分完成，则接口会响应错误。所以在这里获取到的一定是就绪的eip。
#### 输入示例
下面给出一个需要设置源路由的查询结果案例：
```
{
    "publicIP": {
        "eip": "89.13.43.129",
        "mode": "direct"
    }
}
```
* `publicIP`: sbr-eip 插件所使用的公共IP数据域，如果响应的结果中没有包含这个数据域，则sbr-eip什么也不做。
* `publicIP.eip`: 要设置源路由的IP
* `publicIP.mode`: direct 意为开启EIP直通，只有开启了EIP直通，才会尝试设置指定源地址路由。

### 结果示例
#### ADD
通过sbr-eip 插件设置 89.13.43.129 EIP直通后，在容器内产生类似于如下效果。

```
# 将EIP设置为eth0的主IP
ip addr add 89.13.43.129/32 dev eth0

# 创建新的路由表，所有源地址是EIP的数据包都查询新的路由表
ip rule add from 89.13.43.129/32 table 100

# 为EIP设置默认路由
ip route add default via 192.168.1.1 dev net1 table 100
```
#### DEL
执行删除动作会清理容器的辅助IP和源路由

## Example configurations

下面是一个最简单的示例配置


```json
{
            "name":"podlink",
            "cniVersion":"0.4.0",
            "plugins":[
                {
                    "type":"exclusive-device",
                    "ipam":{
                        "type":"enim"
                    }
                },
                {
                    "type":"cilium-cni"
                },
                {
                    "type":"sbr-eip"
                }
            ]
        }
```
