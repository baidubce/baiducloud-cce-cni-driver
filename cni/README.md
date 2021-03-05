# CNI

CCE 维护的 CNI 插件，部署在节点上 /opt/cni/bin 目录下，包括：
- ipvlan: 在容器中创建 ipvlan 网卡
- ptp: 创建 veth pair
- unnumbered-ptp: 创建 veth pair, 配合 ipvlan 打通容器到宿主机的网络
- sysctl: 配置容器中网络相关 sysctl 参数
- eni-ipam: ipam 插件，与 cce-eni-ipam 组件通信申请/释放弹性网卡的辅助 IP