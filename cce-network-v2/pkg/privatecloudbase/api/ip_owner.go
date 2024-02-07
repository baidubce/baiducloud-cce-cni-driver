package api

import (
	"fmt"
	"strings"
)

// NodePoolID This ID represents the IP used by the node pool
func NodePoolID(name, uuid string) string {
	return fmt.Sprintf("node/%s/%s", name, uuid)
}

// PodID owner format with namespace/name
func PodID(owner string) string {
	return fmt.Sprintf("pod/%s", owner)
}

func ISPodID(id string) bool {
	arr := strings.Split(id, "/")
	return len(arr) == 3 && arr[0] == "pod"
}

func GetNodeIDName(id string) string {
	arr := strings.Split(id, "/")
	if len(arr) == 3 && arr[0] == "node" {
		return arr[1]
	}
	return ""
}

// IDToNamespaceName Restore ID to namespace and name
func IDToNamespaceName(id string) (string, string) {
	if ISPodID(id) {
		arr := strings.Split(id, "/")
		return arr[1], arr[2]
	}
	return "", ""
}
