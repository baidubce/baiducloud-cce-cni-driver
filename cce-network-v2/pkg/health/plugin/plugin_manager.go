package plugin

import (
	"fmt"
)

// HealthPlugin implemented by each plugin
type HealthPlugin interface {
	Check() error
}

// HealthPluginManager manage plugin registration and delegate calls
type healthPluginManager map[string]HealthPlugin

var globalHealthPluginManager healthPluginManager = make(healthPluginManager, 0)

// RegisterPlugin registers a plugin
func RegisterPlugin(name string, plugin HealthPlugin) {
	globalHealthPluginManager[name] = plugin
}

// GlobalHealthManager verifies plugins for health of the plugins
func GlobalHealthManager() HealthPlugin {
	return globalHealthPluginManager
}

func (manager healthPluginManager) Check() error {
	for name, plugin := range manager {
		err := plugin.Check()
		if err != nil {
			return fmt.Errorf("%s's health plugin check failed:%v", name, err)
		}
	}
	if len(manager) == 0 {
		return fmt.Errorf("no health plugin found")
	}
	return nil
}
