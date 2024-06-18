package nodediscovery

import (
	"context"
	"fmt"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/comparator"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	k8sCM = controller.NewManager()
)

var _ subscriber.Node = &NodeDiscovery{}

// OnAddNode implements subscriber.Node.
func (u *NodeDiscovery) OnAddNode(node *corev1.Node, wg *lock.StoppableWaitGroup) error {
	return u.updateNetResourceSet(node)
}

// OnDeleteNode implements subscriber.Node.
func (*NodeDiscovery) OnDeleteNode(*corev1.Node, *lock.StoppableWaitGroup) error {
	return nil
}

// OnUpdateNode implements subscriber.Node.
func (u *NodeDiscovery) OnUpdateNode(oldObj *corev1.Node, newObj *corev1.Node, swg *lock.StoppableWaitGroup) error {
	return u.updateNetResourceSet(newObj)
}

func (u *NodeDiscovery) updateNetResourceSet(node *corev1.Node) error {
	controllerName := fmt.Sprintf("sync-node-with-netresourceset (%v)", node.Name)

	doFunc := func(ctx context.Context) (err error) {
		{
			oldNrs, err := k8s.CCEClient().CceV2().NetResourceSets().Get(context.TODO(), node.GetName(), metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					// skip if the NetResourceSet is not found
					return nil
				}
				return fmt.Errorf("failed to get NetResourceSet resource from k8s: %w", err)
			}
			// copy annotation and label from node to netResourceSet
			netResourceSet := oldNrs.DeepCopy()
			if netResourceSet.Labels == nil {
				netResourceSet.Labels = make(map[string]string)
			}
			for k, v := range node.Labels {
				netResourceSet.Labels[k] = v
			}

			if netResourceSet.Annotations == nil {
				netResourceSet.Annotations = make(map[string]string)
			}
			for k, v := range node.Annotations {
				netResourceSet.Annotations[k] = v
			}

			// If the labels are the same, there is no need to update the NetResourceSet.
			if comparator.MapStringEquals(netResourceSet.Labels, oldNrs.Labels) &&
				comparator.MapStringEquals(netResourceSet.Annotations, oldNrs.Annotations) {
				return nil
			}

			if _, err = k8s.CCEClient().CceV2().NetResourceSets().Update(ctx, netResourceSet, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed to update NetResourceSet labels and annotation: %w", err)
			}
		}

		return nil
	}

	k8sCM.UpdateController(controllerName,
		controller.ControllerParams{
			ErrorRetryBaseDuration: time.Second * 30,
			DoFunc:                 doFunc,
		})

	return nil
}
