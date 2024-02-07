# 健康检查插件
健康检查插件使用插件注册机制，包含一个通用的注册器和若干个由面向不同场景而实现的实际实施健康检查的插件。
应用在agent上的健康检查插件的结果最终会提现在进程的 `/healthz` API。同时operator又包含了一套node condition管理机制：如果发现agent
健康检查失败，就为node的condition设置`NetworkUnavailable`，使节点无法正常参与调度。 

## 使用场景
1. VPC-ENI 模式下，如果node上没有任何可用的ENI，则将节点标记为非ready状态。
2. VPC-Router 模式下，如果node的私有子网未在VPC成功发布，则将节点标记为非ready状态。

# 插件工作机制
## 插件接口

```golang
// HealthPlugin implemented by each plugin
type HealthPlugin interface {
	Check() error
	Name() string
}

```
例如在ENI辅助IP模式下，我们应该设计节点上是否最少有一个ENI就绪的健康检查插件。

## 插件管理器
```golang
// HealthPluginManager manage plugin registration and delegate calls
type healthPluginManager map[string]HealthPlugin

var globalHealthPluginManager healthPluginManager

func RegisterPlugin(name string, plugin HealthPlugin) {
	globalHealthPluginManager[name] = plugin
}

func (manager healthPluginManager)Check() error {
	for name, plugin := range manager {
		err := plugin.Check()
		if  err!= nil {
			return fmt.Errorf("%s's health plugin check failed:%v", name, err)
		}
	}
	return nil
}
```