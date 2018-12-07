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
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

// checkPods reconciles the number and the version of pods belonging to the current NATS cluster.
func (c *Cluster) checkPods() error {
	if err := c.reconcileSize(); err != nil {
		return err
	}
	return c.reconcileVersion()
}

// reconcileSize reconciles the size of the NATS cluster.
func (c *Cluster) reconcileSize() error {
	// Grab an up-to-date list of pods that are currently running.
	// Pending pods may be ignored safely as we have previously made sure no pods are in pending state.
	pods, _, _, err := c.pollPods()
	if err != nil {
		return err
	}

	// Grab the current and desired size of the NATS cluster.
	currentSize := len(pods)
	desiredSize := c.cluster.Spec.Size

	if currentSize > desiredSize {
		// Report that we are scaling the cluster down.
		c.cluster.Status.AppendScalingDownCondition(currentSize, desiredSize)
		// Remove extra pods as required in order to meet the desired size.
		// As we remove each pod, we must update the config secret so that routes are re-computed.
		for idx := currentSize - 1; idx >= desiredSize; idx-- {
			if err := c.tryGracefulPodDeletion(pods[idx]); err != nil {
				return err
			}
			if err := c.updateConfigSecret(); err != nil {
				return err
			}
		}
	}

	if currentSize < desiredSize {
		// Report that we are scaling the cluster up.
		c.cluster.Status.AppendScalingUpCondition(currentSize, desiredSize)
		// Create pods as required in order to meet the desired size.
		// As we create each pod, we must update the config secret so that routes are re-computed.
		for idx := currentSize; idx < desiredSize; idx++ {
			if _, err := c.createPod(); err != nil {
				return err
			}
			if err := c.updateConfigSecret(); err != nil {
				return err
			}
		}
	}

	// Update the reported size before returning.
	c.cluster.Status.SetSize(desiredSize)
	return nil
}

// reconcileVersion reconciles the version of pods belonging to the NATS cluster.
func (c *Cluster) reconcileVersion() error {
	// Grab an up-to-date list of pods that are currently running.
	// Pending pods may be ignored safely as we have previously made sure no pods are in pending state.
	pods, _, _, err := c.pollPods()
	if err != nil {
		return err
	}

	// Grab the current and desired version of the NATS cluster.
	currentVersion := c.cluster.Status.CurrentVersion
	desiredVersion := c.cluster.Spec.Version

	if currentVersion != "" && currentVersion != desiredVersion {
		// Report that we are upgrading the cluster's version.
		c.cluster.Status.AppendUpgradingCondition(currentVersion, desiredVersion)
		// Iterate over pods, upgrading them as necessary.
		for _, pod := range pods {
			if kubernetesutil.GetNATSVersion(pod) != c.cluster.Spec.Version {
				c.maybeUpgradeMgmtService()
				return c.upgradePod(pod)
			}
		}
	}

	// Update the reported cluster version before returning.
	c.cluster.Status.SetCurrentVersion(desiredVersion)
	return nil
}
