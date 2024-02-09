# 升级规则
优先推荐用户升级到 1.8.x或者 1.9.x。所有历史暂未发布的 bugfix ，以每月一次的频率做 bug 修复的发布。

> cce 容器网络插件版本号命名规则：
> 1. 版本号通常采用 a.b.c 三位命名格式
> 2. 第一位版本号 a 的升级意为着组件替换，是不能前向兼容升级。例如 v1 升级 v2，暂时不支持升级（有计划），升级时需要考虑数据迁移、禁止变更、机器重建等复杂的升级机制。
> 3. 第二位版本号 b 的升级意为着新的特性或功能，有可能引入不兼容的升级。例如 2.8.0 -> 2.9.0，在升级时一定要通过对应版本的 helm 包做升级。
> 4. 第三位版本号 c 的升级意为着 bug 修复或者质量优化，不会引入新特性，一定会考虑前向兼容。例如 2.8.3 -> 2.8.4，可以直接替换镜像版本号做升级。

[容器网路组件小版本升级 SOP](upgrade-minor-version.md)

# 1.9
### 1.9.4 [20231007]
1. [bugfix] 修复创建 ippool 时获取到的 kind 为空的问题
## 1.9.3
1. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题
## 1.9.2
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题

# 1.8
### 1.8.8 [20231007]
1. [bugfix] 修复创建 ippool 时获取到的 kind 为空的问题
## 1.8.7 [2023/09/25]
### patch
1. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题

## 1.8.6 [2023/09/22]
### patch
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题

## 1.7
### 1.7.9 [暂未发布]
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题
2. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题

## 1.6
### 1.6.11 [暂未发布]
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题
2. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题

## 1.5
### 1.5.3 [暂未发布]
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题
2. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题

## 1.4
### 1.4.7 [20231007]
1. [bugfix] 修复创建 ippool 时获取到的 kind 为空的问题
### 1.4.6 [20230928]
1. [bugfix] 修复 vpc-eni 模式下，eni 辅助 IP 发生变更时，gc 会误释放已分配 ip 状态的问题
2. [bugfix] 修复 vpc-route 模式下，添加重名 node 概率出现 ippool cidr 过期的问题