// Copyright 2016 The nats-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cluster

import (
	"fmt"
	"sync"
	"time"

	"github.com/pires/nats-operator/pkg/spec"
	"github.com/pires/nats-operator/pkg/util/k8sutil"

	"github.com/Sirupsen/logrus"
	k8sapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/labels"
)

type clusterEventType string

const (
	eventDeleteCluster clusterEventType = "Delete"
	eventModifyCluster clusterEventType = "Modify"

	defaultVersion = "v0.9.4"
)

type clusterEvent struct {
	typ  clusterEventType
	spec spec.ClusterSpec
}

type Cluster struct {
	logger *logrus.Entry

	kclient *unversioned.Client

	status *Status

	spec *spec.ClusterSpec

	name      string
	namespace string

	idCounter int
	eventCh   chan *clusterEvent
	stopCh    chan struct{}
}

func New(c *unversioned.Client, name, ns string, spec *spec.ClusterSpec, stopC <-chan struct{}, wg *sync.WaitGroup) *Cluster {
	return new(c, name, ns, spec, stopC, wg, true)
}

func Restore(c *unversioned.Client, name, ns string, spec *spec.ClusterSpec, stopC <-chan struct{}, wg *sync.WaitGroup) *Cluster {
	return new(c, name, ns, spec, stopC, wg, false)
}

func new(kclient *unversioned.Client, name, ns string, spec *spec.ClusterSpec, stopC <-chan struct{}, wg *sync.WaitGroup, isNewCluster bool) *Cluster {
	if len(spec.Version) == 0 {
		// TODO: set version in spec in apiserver
		spec.Version = defaultVersion
	}
	c := &Cluster{
		logger:    logrus.WithField("pkg", "cluster").WithField("cluster-name", name),
		kclient:   kclient,
		name:      name,
		namespace: ns,
		eventCh:   make(chan *clusterEvent, 100),
		stopCh:    make(chan struct{}),
		spec:      spec,
		status:    &Status{},
	}
	if isNewCluster {
		err := c.createServices()
		if err != nil {
			// todo: do not panic!
			panic("todo:" + err.Error())
		}
	}
	wg.Add(1)
	go c.run(stopC, wg)

	return c
}

func (c *Cluster) Delete() {
	c.send(&clusterEvent{typ: eventDeleteCluster})
}

func (c *Cluster) send(ev *clusterEvent) {
	select {
	case c.eventCh <- ev:
	case <-c.stopCh:
	default:
		panic("TODO: too many events queued...")
	}
}

func (c *Cluster) run(stopC <-chan struct{}, wg *sync.WaitGroup) {
	needDeleteCluster := true

	defer func() {
		if needDeleteCluster {
			c.logger.Infof("deleting cluster")
			c.delete()
		}
		close(c.stopCh)
		wg.Done()
	}()

	for {
		select {
		case <-stopC:
			needDeleteCluster = false
			return
		case event := <-c.eventCh:
			switch event.typ {
			case eventModifyCluster:
				// TODO: we can't handle another upgrade while an upgrade is in progress
				c.logger.Infof("spec update: from: %v to: %v", c.spec, event.spec)
				c.spec = &event.spec
			case eventDeleteCluster:
				return
			}
		case <-time.After(5 * time.Second):
			if c.spec.Paused {
				c.logger.Infof("control is paused, skipping reconcilation")
				continue
			}

			running, pending, err := c.pollPods()
			if err != nil {
				c.logger.Errorf("fail to poll pods: %v", err)
				continue
			}
			if len(pending) > 0 {
				c.logger.Infof("skip reconciliation: running (%v), pending (%v)", k8sutil.GetPodNames(running), k8sutil.GetPodNames(pending))
				continue
			}

			if err := c.reconcile(running); err != nil {
				c.logger.Errorf("fail to reconcile: %v", err)
			}
		}
	}
}

func (c *Cluster) Update(spec *spec.ClusterSpec) {
	anyInterestedChange := false
	if (spec.Size != c.spec.Size) || (spec.Paused != c.spec.Paused) {
		anyInterestedChange = true
	}
	if len(spec.Version) == 0 {
		spec.Version = defaultVersion
	}
	if spec.Version != c.spec.Version {
		anyInterestedChange = true
	}
	if anyInterestedChange {
		c.send(&clusterEvent{
			typ:  eventModifyCluster,
			spec: *spec,
		})
	}
}

func (c *Cluster) delete() {
	option := k8sapi.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app":          "nats",
			"nats_cluster": c.name,
		}),
	}

	pods, err := c.kclient.Pods(c.namespace).List(option)
	if err != nil {
		panic(err)
	}
	for i := range pods.Items {
		if err := c.removePod(pods.Items[i].Name); err != nil {
			panic(err)
		}
	}

	err = c.deleteServices()
	if err != nil {
		// todo: do not panic!
		panic("todo:" + err.Error())
	}
}

func (c *Cluster) createServices() error {
	// create management service
	if _, err := k8sutil.CreateMgmtService(c.kclient, c.name, c.namespace); err != nil {
		if !k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			return err
		}
	}
	// create client service
	if _, err := k8sutil.CreateService(c.kclient, c.name, c.namespace); err != nil {
		if !k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			return err
		}
	}
	return nil
}

func (c *Cluster) deleteServices() error {
	var err error

	// delete management service
	err = k8sutil.DeleteMgmtService(c.kclient, c.name, c.namespace)
	if err != nil {
		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	// delete client service
	err = k8sutil.DeleteService(c.kclient, c.name, c.namespace)
	if err != nil {
		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

func (c *Cluster) createPod() error {
	pod := k8sutil.MakePodSpec(c.name, c.spec)
	return k8sutil.CreateAndWaitPod(c.kclient, c.namespace, pod, 60 * time.Second)
}

func (c *Cluster) removePod(name string) error {
	err := c.kclient.Pods(c.namespace).Delete(name, k8sapi.NewDeleteOptions(0))
	if err != nil {
		if !k8sutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
	}
	return nil
}

func (c *Cluster) pollPods() ([]*k8sapi.Pod, []*k8sapi.Pod, error) {
	podList, err := c.kclient.Pods(c.namespace).List(k8sutil.PodListOpt(c.name))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list running pods: %v", err)
	}

	var running []*k8sapi.Pod
	var pending []*k8sapi.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		switch pod.Status.Phase {
		case k8sapi.PodRunning:
			running = append(running, pod)
		case k8sapi.PodPending:
			pending = append(pending, pod)
		}
	}

	return running, pending, nil
}
