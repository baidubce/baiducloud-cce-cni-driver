# IPAM 开发文档
## 目录结构

```
.
|-- api # agent和operator对外api结构
|-- cmd # ipam 可执行程序
|   |-- agent # ipam agent主程序
|   |-- pcb-ipam # 私有云底座ipam cni插件
|   |-- pcb-macvlan # 私有云底座 macvlan 插件,仅用于测试
|   `-- webhook # ipam 固定IP webhook
|-- deploy # 部署工具
|   |-- dockerfile # dockerfile目录
|   `-- private-cloud-base-ipam # 私有云底座 ipam helm
|-- docs # 文档
|-- hack # 代码生成工具
|-- operator # ipam operator可执行程序
|   |-- api # agent和operator对外api结构
|   |-- cmd # 可执行二进制
|   |-- metrics
|   |-- option
|   `-- watchers # operator 对apiserver的监听器
|-- output # 编译输出
|-- pkg
|   |-- addressing
|   |-- alibabacloud
|   |-- annotation
|   |-- api
|   |-- aws
|   |-- azure
|   |-- backoff
|   |-- byteorder
|   |-- checker
|   |-- cidr
|   |-- cleanup
|   |-- client
|   |-- cni
|   |-- command
|   |-- common
|   |-- comparator
|   |-- completion
|   |-- components
|   |-- controller # 定时任务管理器
|   |-- counter
|   |-- datapath
|   |-- debug
|   |-- defaults
|   |-- endpoint
|   |-- eventqueue
|   |-- flowdebug
|   |-- health
|   |-- inctimer
|   |-- ip
|   |-- ipam # 与厂商无关的 ipam 动态IP分配核心代码
|   |-- k8s # 与k8s交互的统一客户端和infomer代码. 自定义CRD的定义和client也在这个包下
|   |-- lock
|   |-- logging
|   |-- mac
|   |-- math
|   |-- metrics
|   |-- mtu
|   |-- netns
|   |-- node
|   |-- nodediscovery
|   |-- option
|   |-- pidfile
|   |-- pprof
|   |-- privatecloudbase # 私有云底座相关实现
|   |-- rand
|   |-- rate
|   |-- revert
|   |-- safetime
|   |-- serializer
|   |-- set
|   |-- spanstat
|   |-- status
|   |-- sysctl
|   |-- testutils
|   |-- trigger
|   |-- types
|   |-- u8proto
|   |-- version # 软件版本信息,通过go build参数设置
|   |-- versioncheck
|   `-- webhook 
|-- state
`-- tools

```

## 编译
### 配置
编译前应指定软件版本,定义软件版本的方法是手动在 VERSION 文件中写入版本号.例如: `echo 1.0.0 > VERSION`
## 如何构建并推送镜像
```
make build docker
```
上面的命令可以把相关二进制推送到cce的插件镜像仓库,镜像的版本号是 `VERSION` 文件中指定的版本号.