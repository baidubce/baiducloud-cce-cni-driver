package privatecloudbase

const (
	// LabelPrivateCloudBaseTopologySwitchName Private cloud base switch name
	LabelPrivateCloudBaseTopologySwitchName = "topology.kubernetes.io/subnet"

	// PrivateCloudBaseName type name of baidu base private cloud
	PrivateCloudBaseName = "PrivateCloudBase"
)

func GetSubnetIDFromNodeLabels(labels map[string]string) string {
	return labels[LabelPrivateCloudBaseTopologySwitchName]
}
