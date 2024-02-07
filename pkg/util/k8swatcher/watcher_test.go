/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package k8swatcher

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type NodeHandlerMock struct {
	lock sync.Mutex

	state   map[string]*v1.Node
	updated chan []*v1.Node
}

func NewNodeHandlerMock() *NodeHandlerMock {
	h := &NodeHandlerMock{
		state:   make(map[string]*v1.Node),
		updated: make(chan []*v1.Node),
	}

	return h
}

func (h *NodeHandlerMock) SyncNode(nodeKey string, nodeLister corelisters.NodeLister) error {
	h.lock.Lock()
	defer h.lock.Unlock()
	node, err := nodeLister.Get(nodeKey)

	if err != nil {
		if kerrors.IsNotFound(err) {
			delete(h.state, nodeKey)
		} else {
			return err
		}
	} else {
		h.state[nodeKey] = node
	}

	h.emit()
	return nil
}

func (h *NodeHandlerMock) emit() {
	nodes := []*v1.Node{}
	for _, v := range h.state {
		nodes = append(nodes, v)
	}
	h.updated <- nodes
}

func (h *NodeHandlerMock) validate(t *testing.T, expectedNodes []*v1.Node) {
	var nodes []*v1.Node
	for {
		select {
		case nodes = <-h.updated:
			if reflect.DeepEqual(nodes, expectedNodes) {
				return
			}
		case <-time.After(wait.ForeverTestTimeout):
			t.Errorf("Timed out. Expected %#v, Got %#v", expectedNodes, nodes)
			return
		}
	}
}

func TestNodeAddedUpdatedDeleted(t *testing.T) {
	client := fake.NewSimpleClientset()

	stopCh := make(chan struct{})
	defer close(stopCh)

	ctx := context.TODO()
	sharedInformerFactory := informers.NewSharedInformerFactory(client, time.Minute)

	mockHandler := NewNodeHandlerMock()
	config := NewNodeWatcher(sharedInformerFactory.Core().V1().Nodes(), time.Minute)
	config.RegisterEventHandler(mockHandler)

	go sharedInformerFactory.Start(stopCh)
	go config.Run(1, stopCh)

	// Add
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
	}
	client.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	mockHandler.validate(t, []*v1.Node{node})

	// Update
	client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	mockHandler.validate(t, []*v1.Node{node})

	// Delete
	client.CoreV1().Nodes().Delete(ctx, "test-node", metav1.DeleteOptions{})
	mockHandler.validate(t, []*v1.Node{})
}
