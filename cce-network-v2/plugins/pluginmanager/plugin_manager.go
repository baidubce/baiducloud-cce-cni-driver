package pluginmanager

import (
	"encoding/json"
	"fmt"
	"os"

	cniTypes "github.com/containernetworking/cni/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

const (
	pluginNameEndpointProbe   = "endpoint-probe"
	pluginNameCipam           = "cipam"
	pluginNameCptp            = "cptp"
	pluginNameEnim            = "enim"
	pluginNameExclusiveDevice = "exclusive-device"
	pluginNameSbrEIP          = "sbr-eip"
	pluginNameRoce            = "roce"

	// external plugins
	pluginNamePortMap = "portmap"
	// cilium cni chain plugin
	pluginNameCiliumCNI = "cilium-cni"

	// 0.4.0 is the version of CNI spec
	defaultCNIVersion = "0.4.0"
)

var (
	ccePlugins = map[string]CniPlugin{
		pluginNameEndpointProbe: NewCNIPlugin(pluginNameEndpointProbe, nil),
		pluginNameCptp:          newPtpPlugin(),
		pluginNameExclusiveDevice: NewCNIPlugin(pluginNameExclusiveDevice, CniPlugin{
			"ipam": pluginNameEnim,
		}),
		pluginNameSbrEIP: NewCNIPlugin(pluginNameSbrEIP, nil),

		// exteral plugins
		pluginNameCiliumCNI: NewCNIPlugin(pluginNameCiliumCNI, nil),

		// portmap cni plugin is not enabled by default.It is only manually enabled for users
		// who want to use portmap feature.
		// TODO: if Cilium is enabled, it comes with port map capabilities and no longer
		// requires this portmap plugin.
		pluginNamePortMap: NewCNIPlugin(pluginNamePortMap, map[string]interface{}{
			"capabilities": map[string]bool{
				"portMappings": true,
			},
			"externalSetMarkChain": "KUBE-MARK-MASQ",
		}),

		// roce cni plugin is enabled by default. It is only manually disenabled for users
		pluginNameRoce: NewCNIPlugin(pluginNameRoce, nil),
	}

	log = logging.NewSubysLogger("plugin-manager")
)

type CniListConfig struct {
	cniTypes.NetConf `json:",inline"`
	Plugins          []CniPlugin `json:"plugins"`
}

type CniPlugin map[string]interface{}

func NewCNIPlugin(name string, config CniPlugin) CniPlugin {
	plugin := make(CniPlugin)
	plugin["type"] = name
	for k, v := range config {
		plugin[k] = v
	}
	return plugin
}

func (plugin CniPlugin) GetType() string {
	v, _, _ := unstructured.NestedString(plugin, "type")
	return v
}

// Automatically generate network plugin configuration files for CCE
// 1. defautl cptp plugin
//
//		{
//			"name":"cce",
//			"cniVersion":"0.4.0",
//			"plugins":[
//				{
//					"type":"cptp",
//					"ipam":{
//						"type":"cipam",
//					},
//					"mtu": {{ .Values.ccedConfig.mtu }}
//				}
//				{{- range .Values.extplugins }}
//				,{
//					"type": "{{ .type }}"
//				}
//				{{- end }}
//				,{
//					"type": "endpoint-probe"
//				}
//	         ,{
//					"type": "roce"
//				}
//			]
//		}
//
// 2. primary eni plugin
//
//		{
//			"name":"podlink",
//			"cniVersion":"0.4.0",
//			"plugins":[
//				{
//					"type":"exclusive-device",
//					"ipam":{
//						"type":"enim"
//					}
//				}
//				{{- range .Values.extplugins }}
//				,{
//					"type": "{{ .type }}"
//				}
//				{{- end }}
//	         ,{
//					"type": "roce"
//				}
//			]
//		}
func defaultCNIPlugin() *CniListConfig {
	result := &CniListConfig{
		NetConf: cniTypes.NetConf{
			CNIVersion: defaultCNIVersion,
			Name:       "generic-veth",
		},
	}

	// primary eni plugin
	if option.Config.ENI != nil && option.Config.ENI.UseMode == string(ccev2.ENIUseModePrimaryIP) {
		result.Plugins = append(result.Plugins, ccePlugins[pluginNameExclusiveDevice])
	} else {
		// use cptp plugin defalt
		result.Plugins = append(result.Plugins, newPtpPlugin())
	}

	// add roce plugin for RDMA
	if option.Config.EnableRDMA {
		result.Plugins = append(result.Plugins, ccePlugins[pluginNameRoce])
	}

	// add list of extension CNI plugins defined by CCE
	for _, pluginName := range option.Config.ExtCNIPluginsList {
		if _, ok := ccePlugins[pluginName]; !ok {
			log.Errorf("ignored unknown plugin %s", pluginName)
		} else {
			result.Plugins = append(result.Plugins, ccePlugins[pluginName])
		}
	}

	return result
}

// read cni configlist from user defined path
func readExistsCNIConfigList(path string) (result *CniListConfig, err error) {
	result = &CniListConfig{}
	byets, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		err = nil
		log.Infof("cni configlist file %s not exists, this file will be ignored", path)
		return
	}
	if err != nil {
		err = fmt.Errorf("failed to read cni configlist file %s: %w", path, err)
		return
	}

	err = json.Unmarshal(byets, &result)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal cni configlist file %s: %w", path, err)
	}
	return
}

func OverwriteCNIConfigList(path string) (override bool, err error) {
	exsistConfig, err := readExistsCNIConfigList(path)
	if err != nil {
		log.WithError(err).Warnf("ignore cni configlist file %s", path)
		err = nil
	}
	// merge exsistConfig and defaultCNIPlugin
	defaultConfig := defaultCNIPlugin()

	// search for user defined plugins
	if exsistConfig != nil {
		for i := range exsistConfig.Plugins {
			plugin := exsistConfig.Plugins[i]
			if _, ok := ccePlugins[plugin.GetType()]; !ok {
				log.Infof("detected user defined plugin %s", plugin.GetType())
				defaultConfig.Plugins = append(defaultConfig.Plugins, plugin)
			}
		}
	}

	if jsonEqual(exsistConfig.Plugins, defaultConfig.Plugins) {
		return false, nil
	}

	// write to cni configlist file
	byets, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		err = fmt.Errorf("failed to marshal cni configlist: %w", err)
		return
	}

	log.Infof("cni config is: %s", string(byets))
	err = os.WriteFile(path, byets, 0644)
	if err != nil {
		err = fmt.Errorf("failed to write cni configlist: %w", err)
		return
	}

	return true, nil
}

// create new cptp plugin template
func newPtpPlugin() CniPlugin {
	plugin := NewCNIPlugin(pluginNameCptp, CniPlugin{
		"ipam": NewCNIPlugin(pluginNameCipam, nil),
		"mtu":  option.Config.MTU,
	})
	return plugin
}

func jsonEqual(a, b interface{}) bool {
	aBytes, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bBytes, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aBytes) == string(bBytes)
}
