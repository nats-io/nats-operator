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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/pires/nats-operator/pkg/debug"
	"github.com/pires/nats-operator/pkg/garbagecollection"
	"github.com/pires/nats-operator/pkg/spec"
	kubernetesutil "github.com/pires/nats-operator/pkg/util/kubernetes"
	natsutil "github.com/pires/nats-operator/pkg/util/nats"
	"github.com/pires/nats-operator/pkg/util/retryutil"

	"github.com/Sirupsen/logrus"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	reconcileInterval         = 8 * time.Second
	podTerminationGracePeriod = int64(5)
)

type clusterEventType string

const (
	eventDeleteCluster clusterEventType = "Delete"
	eventModifyCluster clusterEventType = "Modify"
)

type clusterEvent struct {
	typ     clusterEventType
	cluster *spec.NatsCluster
}

type Config struct {
	ServiceAccount string

	KubeCli corev1client.CoreV1Interface
}

type Cluster struct {
	logger *logrus.Entry
	// debug logger for self hosted cluster
	debugLogger *debug.DebugLogger

	config Config

	cluster *spec.NatsCluster

	// in memory state of the cluster
	// status is the source of truth after Cluster struct is materialized.
	status        spec.ClusterStatus
	memberCounter int

	eventCh chan *clusterEvent
	stopCh  chan struct{}

	tlsConfig *tls.Config

	gc *garbagecollection.GC
}

func New(config Config, cl *spec.NatsCluster, stopC <-chan struct{}, wg *sync.WaitGroup) *Cluster {
	lg := logrus.WithField("pkg", "cluster").WithField("cluster-name", cl.Name)

	c := &Cluster{
		logger:      lg,
		debugLogger: debug.New(cl.Name),
		config:      config,
		cluster:     cl,
		eventCh:     make(chan *clusterEvent, 100),
		stopCh:      make(chan struct{}),
		status:      cl.Status.Copy(),
		gc:          garbagecollection.New(config.KubeCli, cl.Namespace),
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := c.setup(); err != nil {
			c.logger.Errorf("cluster failed to setup: %v", err)
			if c.status.Phase != spec.ClusterPhaseFailed {
				c.status.SetReason(err.Error())
				c.status.SetPhase(spec.ClusterPhaseFailed)
				if err := c.updateCRStatus(); err != nil {
					c.logger.Errorf("failed to update cluster phase (%v): %v", spec.ClusterPhaseFailed, err)
				}
			}
			return
		}
		c.run(stopC)
	}()

	return c
}

func (c *Cluster) setup() error {
	err := c.cluster.Spec.Validate()
	if err != nil {
		return fmt.Errorf("invalid cluster spec: %v", err)
	}

	var shouldCreateCluster bool
	switch c.status.Phase {
	case spec.ClusterPhaseNone:
		shouldCreateCluster = true
	case spec.ClusterPhaseCreating:
		return errCreatedCluster
	case spec.ClusterPhaseRunning:
		shouldCreateCluster = false

	default:
		return fmt.Errorf("unexpected cluster phase: %s", c.status.Phase)
	}

	if c.isSecureClient() {
		d, err := kubernetesutil.GetTLSDataFromSecret(c.config.KubeCli, c.cluster.Namespace, c.cluster.Spec.TLS.Static.OperatorSecret)
		if err != nil {
			return err
		}
		c.tlsConfig, err = natsutil.NewTLSConfig(d.CertData, d.KeyData, d.CAData)
		if err != nil {
			return err
		}
	}

	if shouldCreateCluster {
		return c.create()
	}
	return nil
}

func (c *Cluster) create() error {
	c.status.SetPhase(spec.ClusterPhaseCreating)

	if err := c.updateCRStatus(); err != nil {
		return fmt.Errorf("cluster create: failed to update cluster phase (%v): %v", spec.ClusterPhaseCreating, err)
	}
	c.logClusterCreation()

	c.gc.CollectCluster(c.cluster.Name, c.cluster.UID)

	if err := c.setupServices(); err != nil {
		return fmt.Errorf("cluster create: fail to create client service LB: %v", err)
	}
	return nil
}

func (c *Cluster) Delete() {
	c.send(&clusterEvent{typ: eventDeleteCluster})
}

func (c *Cluster) send(ev *clusterEvent) {
	select {
	case c.eventCh <- ev:
		l, ecap := len(c.eventCh), cap(c.eventCh)
		if l > int(float64(ecap)*0.8) {
			c.logger.Warningf("eventCh buffer is almost full [%d/%d]", l, ecap)
		}
	case <-c.stopCh:
	}
}

func (c *Cluster) run(stopC <-chan struct{}) {
	clusterFailed := false

	defer func() {
		if clusterFailed {
			c.reportFailedStatus()

			c.logger.Infof("deleting the failed cluster")
			c.delete()
		}

		close(c.stopCh)
	}()

	c.status.SetPhase(spec.ClusterPhaseRunning)
	if err := c.updateCRStatus(); err != nil {
		c.logger.Warningf("update initial CR status failed: %v", err)
	}
	c.logger.Infof("start running...")

	var rerr error
	for {
		select {
		case <-stopC:
			return
		case event := <-c.eventCh:
			switch event.typ {
			case eventModifyCluster:
				if isSpecEqual(event.cluster.Spec, c.cluster.Spec) {
					break
				}
				// TODO: we can't handle another upgrade while an upgrade is in progress
				c.logSpecUpdate(event.cluster.Spec)

				c.cluster = event.cluster

			case eventDeleteCluster:
				c.logger.Infof("cluster is deleted by the user")
				clusterFailed = true
				return
			default:
				panic("unknown event type" + event.typ)
			}

		case <-time.After(reconcileInterval):
			start := time.Now()

			if c.cluster.Spec.Paused {
				c.status.PauseControl()
				c.logger.Infof("control is paused, skipping reconciliation")
				continue
			} else {
				c.status.Control()
			}

			running, pending, err := c.pollPods()
			if err != nil {
				c.logger.Errorf("fail to poll pods: %v", err)
				reconcileFailed.WithLabelValues("failed to poll pods").Inc()
				continue
			}

			if len(pending) > 0 {
				// Pod startup might take long, e.g. pulling image. It would deterministically become running or succeeded/failed later.
				c.logger.Infof("skip reconciliation: running (%v), pending (%v)", kubernetesutil.GetPodNames(running), kubernetesutil.GetPodNames(pending))
				reconcileFailed.WithLabelValues("not all pods are running").Inc()
				continue
			}
			if len(running) == 0 {
				c.logger.Error("all NATS pods are dead")
				break
			}

			rerr = c.reconcile(running)
			if rerr != nil {
				c.logger.Errorf("failed to reconcile: %v", rerr)
				break
			}

			reconcileHistogram.WithLabelValues(c.name()).Observe(time.Since(start).Seconds())
		}

		if rerr != nil {
			reconcileFailed.WithLabelValues(rerr.Error()).Inc()
		}

		if isFatalError(rerr) {
			clusterFailed = true
			c.status.SetReason(rerr.Error())

			c.logger.Errorf("cluster failed: %v", rerr)
			return
		}
	}
}

func isSpecEqual(s1, s2 spec.ClusterSpec) bool {
	if s1.Size != s2.Size || s1.Paused != s2.Paused || s1.Version != s2.Version {
		return false
	}
	return true
}

func (c *Cluster) isSecurePeer() bool {
	return c.cluster.Spec.TLS.IsSecurePeer()
}

func (c *Cluster) isSecureClient() bool {
	return c.cluster.Spec.TLS.IsSecureClient()
}

func (c *Cluster) Update(cl *spec.NatsCluster) {
	c.send(&clusterEvent{
		typ:     eventModifyCluster,
		cluster: cl,
	})
}

func (c *Cluster) delete() {
	c.gc.CollectCluster(c.cluster.Name, garbagecollection.NullUID)
}

func (c *Cluster) setupServices() error {
	err := kubernetesutil.CreateClientService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, c.cluster.AsOwner())
	if err != nil {
		return err
	}

	return kubernetesutil.CreateMgmtService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, c.cluster.AsOwner())
}

func (c *Cluster) createPod() error {
	pod := kubernetesutil.NewNatsPodSpec(c.cluster.Name, c.cluster.Spec, c.cluster.AsOwner())

	_, err := c.config.KubeCli.Pods(c.cluster.Namespace).Create(pod)

	return err
}

func (c *Cluster) removePod(name string) error {
	ns := c.cluster.Namespace
	opts := metav1.NewDeleteOptions(podTerminationGracePeriod)
	err := c.config.KubeCli.Pods(ns).Delete(name, opts)
	if err != nil {
		if !kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
		if c.isDebugLoggerEnabled() {
			c.debugLogger.LogMessage(fmt.Sprintf("pod (%s) not found while trying to delete it", name))
		}
	}
	if c.isDebugLoggerEnabled() {
		c.debugLogger.LogPodDeletion(name)
	}
	return nil
}

func (c *Cluster) pollPods() (running, pending []*v1.Pod, err error) {
	podList, err := c.config.KubeCli.Pods(c.cluster.Namespace).List(kubernetesutil.ClusterListOpt(c.cluster.Name))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list running pods: %v", err)
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		if len(pod.OwnerReferences) < 1 {
			c.logger.Warningf("pollPods: ignore pod %v: no owner", pod.Name)
			continue
		}
		if pod.OwnerReferences[0].UID != c.cluster.UID {
			c.logger.Warningf("pollPods: ignore pod %v: owner (%v) is not %v",
				pod.Name, pod.OwnerReferences[0].UID, c.cluster.UID)
			continue
		}
		switch pod.Status.Phase {
		case v1.PodRunning:
			running = append(running, pod)
		case v1.PodPending:
			pending = append(pending, pod)
		}
	}

	return running, pending, nil
}

func (c *Cluster) updateCRStatus() error {
	if reflect.DeepEqual(c.cluster.Status, c.status) {
		return nil
	}

	newCluster := c.cluster
	newCluster.Status = c.status
	newCluster, err := kubernetesutil.UpdateClusterTPRObject(c.config.KubeCli.RESTClient(), c.cluster.Namespace, newCluster)
	if err != nil {
		return fmt.Errorf("failed to update CR status: %v", err)
	}

	c.cluster = newCluster

	return nil
}

func (c *Cluster) reportFailedStatus() {
	retryInterval := 5 * time.Second

	f := func() (bool, error) {
		c.status.SetPhase(spec.ClusterPhaseFailed)
		err := c.updateCRStatus()
		if err == nil || kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			return true, nil
		}

		if !apierrors.IsConflict(err) {
			c.logger.Warningf("retry report status in %v: fail to update: %v", retryInterval, err)
			return false, nil
		}

		cl, err := kubernetesutil.GetClusterTPRObject(c.config.KubeCli.RESTClient(), c.cluster.Namespace, c.cluster.Name)
		if err != nil {
			// Update (PUT) will return conflict even if object is deleted since we have UID set in object.
			// Because it will check UID first and return something like:
			// "Precondition failed: UID in precondition: 0xc42712c0f0, UID in object meta: ".
			if kubernetesutil.IsKubernetesResourceNotFoundError(err) {
				return true, nil
			}
			c.logger.Warningf("retry report status in %v: fail to get latest version: %v", retryInterval, err)
			return false, nil
		}
		c.cluster = cl
		return false, nil

	}

	retryutil.Retry(retryInterval, math.MaxInt64, f)
}

func (c *Cluster) name() string {
	return c.cluster.GetName()
}

func (c *Cluster) logClusterCreation() {
	specBytes, err := json.MarshalIndent(c.cluster.Spec, "", "    ")
	if err != nil {
		c.logger.Errorf("failed to marshal cluster spec: %v", err)
	}

	c.logger.Info("creating cluster with Spec:")
	for _, m := range strings.Split(string(specBytes), "\n") {
		c.logger.Info(m)
	}
}

func (c *Cluster) logSpecUpdate(newSpec spec.ClusterSpec) {
	oldSpecBytes, err := json.MarshalIndent(c.cluster.Spec, "", "    ")
	if err != nil {
		c.logger.Errorf("failed to marshal cluster spec: %v", err)
	}
	newSpecBytes, err := json.MarshalIndent(newSpec, "", "    ")
	if err != nil {
		c.logger.Errorf("failed to marshal cluster spec: %v", err)
	}

	c.logger.Infof("spec update: Old Spec:")
	for _, m := range strings.Split(string(oldSpecBytes), "\n") {
		c.logger.Info(m)
	}

	c.logger.Infof("New Spec:")
	for _, m := range strings.Split(string(newSpecBytes), "\n") {
		c.logger.Info(m)
	}

	if c.isDebugLoggerEnabled() {
		c.debugLogger.LogClusterSpecUpdate(string(oldSpecBytes), string(newSpecBytes))
	}
}

func (c *Cluster) isDebugLoggerEnabled() bool {
	if c.debugLogger != nil {
		return true
	}
	return false
}
