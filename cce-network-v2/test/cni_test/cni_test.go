package cnitest

import (
	"os/exec"
	"testing"
)

func BenchmarkCNIPlugin(b *testing.B) {
	for i := 0; i < b.N; i++ {
		exec.Command("ls", "-al", "/root/codes/baidu_jpaas-caas_baiducloud-cce-cni-driver/baidu/jpaas-caas/baiducloud-cce-cni-driver/cce-network-v2").Run()
	}
}
