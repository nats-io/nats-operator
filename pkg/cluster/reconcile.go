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
	c.logger.Debugln("Start reconciling...")
	var err error

	switch {
	case len(pods) != c.spec.Size:
		err = c.reconcileSize(pods)
	case needsUpgrade(pods, c.spec):
		c.status.upgradeVersionTo(c.spec.Version)
		err = c.reconcileUpgrade(pods, c.spec)
	}

	c.logger.Debugln("Finished reconciling.")
	return err
}

// reconcileSize reconciles the size of cluster.
func (c *Cluster) reconcileSize(pods []*api.Pod) error {
	c.logger.Warningf("Cluster size needs reconciling: expected %d, has %d", c.spec.Size, len(pods))
	// do we need to add or remove pods?
	if len(pods) < c.spec.Size {
		if err := c.createAndWaitForPod(); err != nil {
			return err
		}
	} else if len(pods) < c.spec.Size {
		if err := c.removePod(pods[len(pods)-1].Name); err != nil {
			c.logger.Error(err)
		}
	}

	return nil
}

func (c *Cluster) reconcileUpgrade(pods []*api.Pod, cs *spec.ClusterSpec) error {
	c.logger.Warningf("Cluster version doesn't match, reconciling...")
	return c.upgradeAndWaitForPod(pickPodToUpgrade(pods, cs.Version))
}

// needsUpgrade determines whether cluster needs upgrade or not.
func needsUpgrade(pods []*api.Pod, cs *spec.ClusterSpec) bool {
	return len(pods) == cs.Size && pickPodToUpgrade(pods, cs.Version) != nil
}

// pickExistingPeer selects the first pod, if any, which version doesn't
// correspond to desired version.
func pickPodToUpgrade(pods []*api.Pod, newVersion string) *api.Pod {
	for _, pod := range pods {
		if k8sutil.GetNATSVersion(pod) == newVersion {
			continue
		}
		return pod
	}
	return nil
}
