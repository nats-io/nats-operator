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
	"github.com/pires/nats-operator/pkg/spec"
	"github.com/pires/nats-operator/pkg/util/k8sutil"

	"k8s.io/kubernetes/pkg/api"
)

// reconcile reconciles cluster current state to desired state specified by spec.
// - it tries to reconcile the cluster to desired size.
// - if the cluster needs upgrade, it tries to upgrade existing peers, one by one.
func (c *Cluster) reconcile(pods []*api.Pod) error {
	c.logger.Infoln("Start reconciling")
	defer c.logger.Infoln("Finish reconciling")

	switch {
	case len(pods) != c.spec.Size:
		return c.reconcileSize(pods)
	case needsUpgrade(pods, c.spec):
		c.status.upgradeVersionTo(c.spec.Version)
		return reconcileUpgrade(pods, c.spec)
	default:
		c.status.setVersion(c.spec.Version)
		return nil
	}
}

// reconcileSize reconciles the size of cluster.
func (c *Cluster) reconcileSize(pods []*api.Pod) error {
	// do we need to add or remove pods?
	if len(pods) < c.spec.Size {
		podsToAdd := c.spec.Size - len(pods)
		for i := 0; i < podsToAdd; i++ {
			// one at a time, sequentially
			if err := c.addOnePeer(); err != nil {
				c.logger.Error(err)
			} else {

			}
		}
	} else if len(pods) < c.spec.Size {
		podsToRemove := len(pods) - c.spec.Size
		for i := podsToRemove; i > 0; i-- {
			if err := c.removeOnePeer(pods[len(pods)-1]); err != nil {
				c.logger.Error(err)
			}
		}
	}

	return nil
}

func (c *Cluster) addOnePeer() error {
	if err := c.createAndWaitForPod(); err != nil {
		c.logger.Errorf("failed to create new peer: %v", err)
		return err
	}

	return nil
}

func (c *Cluster) removeOnePeer(pod *api.Pod) error {
	if err := c.removePod(pod.Name); err != nil {
		return err
	}

	return nil
}

func reconcileUpgrade(pods []*api.Pod, cs *spec.ClusterSpec) error {
	// TODO implement

	return nil
}

// needsUpgrade determines whether cluster needs upgrade or not.
func needsUpgrade(pods []*api.Pod, cs *spec.ClusterSpec) bool {
	return len(pods) == cs.Size && pickPeerToUpgrade(pods, cs.Version) != nil
}

// pickExistingPeer selects the first pod, if any, which version doesn't
// correspond to desired version.
func pickPeerToUpgrade(pods []*api.Pod, newVersion string) *api.Pod {
	for _, pod := range pods {
		if k8sutil.GetNATSVersion(pod) == newVersion {
			continue
		}
		return pod
	}
	return nil
}
