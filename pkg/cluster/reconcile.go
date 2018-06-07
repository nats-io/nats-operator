// Copyright 2017 The nats-operator Authors
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
	"github.com/nats-io/nats-operator/pkg/spec"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"

	"k8s.io/api/core/v1"
)

// reconcile reconciles cluster current state to desired state specified by spec.
// - it tries to reconcile the cluster to desired size.
// - if the cluster needs upgrade, it tries to upgrade existing peers, one by one.
func (c *Cluster) reconcile(pods []*v1.Pod) error {
	c.logger.Debugln("Start reconciling...")
	defer c.logger.Debugln("Finish reconciling")

	spec := c.cluster.Spec

	clusterNeedsResize := len(pods) != spec.Size
	clusterNeedsUpgrade := needsUpgrade(pods, spec)

	if clusterNeedsResize {
		return c.reconcileSize(pods)
	}
	if clusterNeedsUpgrade {
		return c.reconcileUpgrade(pods, spec)
	}

	c.status.SetCurrentVersion(spec.Version)
	c.status.SetReadyCondition()

	return nil
}

// reconcileSize reconciles the size of cluster.
func (c *Cluster) reconcileSize(pods []*v1.Pod) error {
	spec := c.cluster.Spec

	c.logger.Infof("Cluster size needs reconciling: expected %d, has %d", spec.Size, len(pods))

	currentClusterSize := len(pods)
	if currentClusterSize < spec.Size {
		c.status.AppendScalingUpCondition(currentClusterSize, c.cluster.Spec.Size)
		_, err := c.createPod()
		if err != nil {
			return err
		}
		if err = c.updateConfigMap(); err != nil {
			c.logger.Warningf("error updating the shared config map: %s", err)
		}

	} else if currentClusterSize > spec.Size {
		c.status.AppendScalingDownCondition(currentClusterSize, c.cluster.Spec.Size)
		if err := c.removePod(pods[currentClusterSize-1].Name); err != nil {
			return err
		}
		if err := c.updateConfigMap(); err != nil {
			c.logger.Warningf("error updating the shared config map: %s", err)
		}
	}

	return nil
}

func (c *Cluster) reconcileUpgrade(pods []*v1.Pod, cs spec.ClusterSpec) error {
	c.logger.Warningf("Cluster version doesn't match, reconciling...")
	pod := pickPodToUpgrade(pods, cs.Version)
	kubernetesutil.SetNATSVersion(pod, cs.Version)
	c.maybeUpgradeMgmtService()
	return c.upgradePod(pod)
}

// needsUpgrade determines whether cluster needs upgrade or not.
func needsUpgrade(pods []*v1.Pod, cs spec.ClusterSpec) bool {
	return len(pods) == cs.Size && pickPodToUpgrade(pods, cs.Version) != nil
}

// pickPodToUpgrade selects the first pod, if any, which version doesn't
// correspond to desired version.
func pickPodToUpgrade(pods []*v1.Pod, newVersion string) *v1.Pod {
	for _, pod := range pods {
		if kubernetesutil.GetNATSVersion(pod) != newVersion {
			return pod
		}
	}
	return nil
}
