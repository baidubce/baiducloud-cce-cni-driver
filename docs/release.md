# 1.9
## 1.9.3
1. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题
## 1.9.2
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题

# 1.8
## 1.8.7 [2023/09/25]
### patch
1. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题

## 1.8.6 [2023/09/22]
### patch
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题