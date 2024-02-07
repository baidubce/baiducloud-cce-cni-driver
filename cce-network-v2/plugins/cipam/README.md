# cipam
cce ipam
用于连接cce-network-v2-agent的CNI插件，支持标准CNI IPAM的协议，下层可以对接通用的CNI标准插件。提供对单网卡的IP地址分配功能。

## 配置示例
```
{
  "name":"macvlan-ipam",
  "cniVersion":"0.4.0",
  "plugins":[
    {
      "type":"macvlan",
      "mtu": 1500,
      "master":"eth0",
      "serviceCIDR": "10.1.104.0/22",
      // ipam 日志路径, 默认为空
      // "log-file": ""
      "ipam":{
        // 指定插件类型为 cipam
        "type":"cipam",
        // 下面的所有属性都仅用于测试使用
        // 是否开启cni调试模式 
        // "enable-debug": false,
        // 复制宿主机上的路由配置,该选项仅用于测试
        // "use-host-gate-way": false,
        // 从哪个网卡复制路由数据
        // "hostLink":"m0",
        // 手动配置路由
        // "routes": [
        //          {
        //            "dst": "0.0.0.0/0",
        //            "gw":"172.16.8.1",
        //          }
        //        ]
      }
    }
  ]
}

```

## 应用场景
cce-ipam 是一个通用的IPAM插件，它可以支持以下场景使用：
* 智能云 CCE vpc-eni 模式的容器网络
* 智能云 CCE vpc 路由模式的容器网络
* 百度私有云底座容器网络