# ENIM 管理器

主要为分配ENI管理器时使用，该管理器主要适用于独占ENI的场景。

ENI管理器的角色类似于IPAM，它是嵌入在agent组件中，直接处理CNI插件的ADD和DEL CNI的请求。
在系统内部，ENI管理器会处理以下几类任务：
1. ENI 分配： 记录当前使用ENI的网络端点信息
2. ENI 删除： 以接口复用的方式，将位于定制网络命名空间中的接口，移动到初始命名空间。
3. ENI 回收： 以定时任务的方式运行。主要处理当容器被异常销毁时，ENI的回收逻辑。

ENI管理器的接口定义如下：

```
type ENIManager interface{
	// ADD select an available ENI from the ENI list and assign it to the 
	// network endpoint
	ADD(namespace, name string) (*models.IPAMAddressResponse, *models.IPAMAddressResponse)

	// DEL  recycle the endpoint has been recycled and needs to trigger the 
	// reuse of ENI
	DEL(namespace, name string) error
	
	// GC the eni manager needs to implement the garbage cleaning logic for 
	// the endpoint. When the endpoint no longer exists, it needs to trigger 
	// the ENI release logic.
	// Warning When releasing ENI, it also involves device management. It is 
	// necessary to move the network device put into the container namespace 
	// to the host namespace, and rename the device when moving.
	GC()error
}
```

eni 管理器需要实现对endpoint的垃圾清理逻辑，当endpoint已经不存在时，需要触发ENI的释放逻辑。
警告在释放ENI时还涉及到了设备管理，需要把放入到容器命名空间的网络设备，移动到宿主机命名空间，并需要在移动时，对设备执行重命名。

# ENI管理提供者
在设计上，ENI管理器提供者由各个云厂商（如BCE）提供实现。其接口定义如下：

```
// ENIProvider manages ENI configuration for different platforms
type ENIProvider interface {
	// List all available ENI
	List() []*ccev2.ENI

	// AllocateNextENI select an available ENI from the ENI list and assign it 
	// to the network endpoint
	// Warning that the provider must implement the resource competition mechanism 
	// to ensure that the resource application can be processed correctly when the 
	// allocate and deallocate are concurrent.
	AllocateNextENI(namespace, name string) (*ccev2.ENI, error)

	// DeallocateENI frees up a resource assigned to a network endpoint
	// If no endpoint uses ENI, the provider is also required to return success
	DeallocateENI(namespace, name string) error
}
```
ENI管理提供者的核心功能如下：
1. 提供ENI快照功能，供ENI管理器做基于Endpoint的垃圾收集使用
2. 申请新的ENI。ENI提供者的基本能力，为管理器提供一个就绪的ENI设备。
3. 释放ENI，使ENI重新变为可用状态

注意ENI的申请和释放有并发的可能，所以需要提供者必要的情况下对资源做竞争保护。