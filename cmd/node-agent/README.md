# cce-cni-node-agent

cce-cni-node-agent 运行在每个节点上，负责的功能包括：
- 获取节点对应的 ipam spec 信息，维护 ippool CRD; 
- 根据 ipam spec, 维护节点上 cni 配置文件；（TODO）
- 容器出公网 IP 的 MASQ 规则配置； （TODO， 当前可以用单独部署的 ip-masq-agent 代替）
- VPC-Native 模式下挂载 ENI，以及维护 ENI 的部分路由规则；
- network policy (TODO)
