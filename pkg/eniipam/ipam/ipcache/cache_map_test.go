package ipcache

import (
	"testing"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	"github.com/stretchr/testify/suite"
)

type ipcacheTest struct {
	suite.Suite
}

func (suite *ipcacheTest) TestMap() {
	pool := NewReuseIPAndWepPool()

	wep := data.MockFixedWorkloadEndpoint()
	wep.Spec.IPv6 = "::1"
	pool.OnAdd(wep)
	suite.Truef(pool.Exists(wep.Spec.IP), "wep in map")
	_, ok := pool.Get(wep.Spec.IPv6)
	suite.Truef(ok, "wep 6 in map")
	pool.OnUpdate(wep, wep)
	pool.OnDelete(wep)

	pool.AddIfNotExists("10.0.0.1", wep)
	pool.AddIfNotExists("", wep)
	pool.ForEach(func(key string, item *networkingv1alpha1.WorkloadEndpoint) bool {
		return false
	})
}

func (suite *ipcacheTest) TestMapArray() {
	pool := NewCacheMapArray[int]()

	pool.Append("a", 0)
	pool.Append("", 0)
	if suite.Truef(pool.Exists("a"), "wep in map") {
		pool.Get("a")
	}
	pool.ForEach(func(key string, item []int) bool {
		return false
	})
	pool.ForEachSubItem(func(key string, index, item int) bool {
		return false
	})
	pool.Delete("0")
}

func TestCacheMap(t *testing.T) {
	suite.Run(t, new(ipcacheTest))
}
