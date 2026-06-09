/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package watchers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	pkgOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	applyv1 "k8s.io/client-go/applyconfigurations/core/v1"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listv1 "k8s.io/client-go/listers/core/v1"
)

func TestAddNodeTaint_Given_NoExistingTaints_When_AddNodeTaint_Then_PatchAdd(t *testing.T) {
	nodeName := "test-node-empty-taints"
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}

	mockLister := &mockNodeLister{node: node}
	mockInterface := &mockNodeInterface{}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(NodeClient, "Lister", func(*NodeUpdater) listv1.NodeLister {
		return mockLister
	})
	patches.ApplyMethod(NodeClient, "NodeInterface", func(*NodeUpdater) typev1.NodeInterface {
		return mockInterface
	})

	err := addNodeTaint(context.Background(), nodeName)
	assert.NoError(t, err)
	assert.Equal(t, 1, mockInterface.patchCalls)
	assert.Equal(t, types.JSONPatchType, mockInterface.patchType)

	var ops []k8s.JSONPatch
	err = json.Unmarshal(mockInterface.patchData, &ops)
	assert.NoError(t, err)
	if assert.Len(t, ops, 1) {
		assert.Equal(t, "add", ops[0].OP)
		assert.Equal(t, "/spec/taints", ops[0].Path)
		taints := decodeTaints(t, ops[0].Value)
		if assert.Len(t, taints, 1) {
			assert.Equal(t, pkgOption.Config.AgentNotReadyNodeTaintValue(), taints[0].Key)
			assert.Equal(t, corev1.TaintEffectNoSchedule, taints[0].Effect)
		}
	}
}

func TestAddNodeTaint_Given_ExistingTaints_When_AddNodeTaint_Then_PatchReplace(t *testing.T) {
	nodeName := "test-node-existing-taints"
	existingTaint := corev1.Taint{Key: "other-taint", Effect: corev1.TaintEffectNoExecute}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{existingTaint},
		},
	}

	mockLister := &mockNodeLister{node: node}
	mockInterface := &mockNodeInterface{}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(NodeClient, "Lister", func(*NodeUpdater) listv1.NodeLister {
		return mockLister
	})
	patches.ApplyMethod(NodeClient, "NodeInterface", func(*NodeUpdater) typev1.NodeInterface {
		return mockInterface
	})

	err := addNodeTaint(context.Background(), nodeName)
	assert.NoError(t, err)
	assert.Equal(t, 1, mockInterface.patchCalls)

	var ops []k8s.JSONPatch
	err = json.Unmarshal(mockInterface.patchData, &ops)
	assert.NoError(t, err)
	if assert.Len(t, ops, 2) {
		assert.Equal(t, "test", ops[0].OP)
		assert.Equal(t, "/spec/taints", ops[0].Path)
		current := decodeTaints(t, ops[0].Value)
		if assert.Len(t, current, 1) {
			assert.Equal(t, existingTaint.Key, current[0].Key)
			assert.Equal(t, existingTaint.Effect, current[0].Effect)
		}

		assert.Equal(t, "replace", ops[1].OP)
		assert.Equal(t, "/spec/taints", ops[1].Path)
		updated := decodeTaints(t, ops[1].Value)
		if assert.Len(t, updated, 2) {
			assert.Equal(t, existingTaint.Key, updated[0].Key)
			assert.Equal(t, existingTaint.Effect, updated[0].Effect)
			assert.Equal(t, pkgOption.Config.AgentNotReadyNodeTaintValue(), updated[1].Key)
			assert.Equal(t, corev1.TaintEffectNoSchedule, updated[1].Effect)
		}
	}
}

func TestAddNodeTaint_Given_TaintAlreadyPresent_When_AddNodeTaint_Then_NoPatch(t *testing.T) {
	nodeName := "test-node-taint-present"
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{newAgentNotReadyTaint()},
		},
	}

	mockLister := &mockNodeLister{node: node}
	mockInterface := &mockNodeInterface{}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(NodeClient, "Lister", func(*NodeUpdater) listv1.NodeLister {
		return mockLister
	})
	patches.ApplyMethod(NodeClient, "NodeInterface", func(*NodeUpdater) typev1.NodeInterface {
		return mockInterface
	})

	err := addNodeTaint(context.Background(), nodeName)
	assert.NoError(t, err)
	assert.Equal(t, 0, mockInterface.patchCalls)
}

func TestAddNodeTaint_Given_ListerError_When_AddNodeTaint_Then_ReturnError(t *testing.T) {
	mockLister := &mockNodeLister{err: errors.New("mock lister error")}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(NodeClient, "Lister", func(*NodeUpdater) listv1.NodeLister {
		return mockLister
	})

	err := addNodeTaint(context.Background(), "test-node")
	assert.Error(t, err)
}

func decodeTaints(t *testing.T, value interface{}) []corev1.Taint {
	t.Helper()
	raw, err := json.Marshal(value)
	assert.NoError(t, err)
	var taints []corev1.Taint
	err = json.Unmarshal(raw, &taints)
	assert.NoError(t, err)
	return taints
}

func newAgentNotReadyTaint() corev1.Taint {
	return corev1.Taint{
		Key:    pkgOption.Config.AgentNotReadyNodeTaintValue(),
		Effect: corev1.TaintEffectNoSchedule,
	}
}

type mockNodeLister struct {
	node *corev1.Node
	err  error
}

func (m *mockNodeLister) List(selector labels.Selector) ([]*corev1.Node, error) {
	return nil, nil
}

func (m *mockNodeLister) Get(name string) (*corev1.Node, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.node, nil
}

type mockNodeInterface struct {
	patchCalls int
	patchType  types.PatchType
	patchData  []byte
	patchError error
}

func (m *mockNodeInterface) Create(ctx context.Context, node *corev1.Node, opts metav1.CreateOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) Update(ctx context.Context, node *corev1.Node, opts metav1.UpdateOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) UpdateStatus(ctx context.Context, node *corev1.Node, opts metav1.UpdateOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return errors.New("not implemented")
}

func (m *mockNodeInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return errors.New("not implemented")
}

func (m *mockNodeInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) List(ctx context.Context, opts metav1.ListOptions) (*corev1.NodeList, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*corev1.Node, error) {
	m.patchCalls++
	m.patchType = pt
	m.patchData = data
	return &corev1.Node{}, m.patchError
}

func (m *mockNodeInterface) Apply(ctx context.Context, node *applyv1.NodeApplyConfiguration, opts metav1.ApplyOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) ApplyStatus(ctx context.Context, node *applyv1.NodeApplyConfiguration, opts metav1.ApplyOptions) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}

func (m *mockNodeInterface) PatchStatus(ctx context.Context, nodeName string, data []byte) (*corev1.Node, error) {
	return nil, errors.New("not implemented")
}
