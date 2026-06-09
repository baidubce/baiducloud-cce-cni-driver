package agent

import (
	"testing"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/stretchr/testify/assert"
)

func TestENIInitFactoryCheck(t *testing.T) {
	tests := []struct {
		name          string
		localENICount int
		expectError   bool
	}{
		{
			name:          "should fail when local enis empty",
			localENICount: 0,
			expectError:   true,
		},
		{
			name:          "should pass when local enis exist",
			localENICount: 1,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			localENIs := make(map[string]*ccev2.ENI)
			for i := 0; i < tt.localENICount; i++ {
				localENIs["eni"] = &ccev2.ENI{}
			}

			factory := &eniInitFactory{
				localENIs: localENIs,
			}

			err := factory.Check()
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
