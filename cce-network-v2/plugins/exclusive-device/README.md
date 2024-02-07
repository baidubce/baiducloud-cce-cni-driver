CCE Exclusive Device cni plugin, supporting both ENI and ERI

# demo

cni 配置文件 `/etc/cni/net.d/00-cce.conflist`
```
{
    "name":"exclude-eni",
    "cniVersion":"1.0.0",
    "plugins":[
        {
            "type":"exclusive-device"
            "ipam":{
                "type":"enim"
            }
        }
    ]
}
```