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
	"context"
	"fmt"

	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// upgradePod upgrades the specified pod to the desired version for the current NATS cluster.
// In order to do that, we first try to make NATS enter the "lame duck" mode.
// If we succeed, we adopt a special upgrade procedure since the pod (or at least its "nats" container) will have been terminated and can't be upgraded directly.
// If we fail, we stick to the usual method of upgrading the container's "image" field to the desired version.
func (c *Cluster) upgradePod(pod *v1.Pod) error {
	if err := c.enterLameDuckModeAndWaitTermination(pod); err != nil {
		c.logger.Warn(err)
		return c.upgradeRunningPod(pod)
	}
	return c.upgradeTerminatedPod(pod)
}

func (c *Cluster) upgradeRunningPod(oldPod *v1.Pod) error {
	ns := c.cluster.Namespace

	pod, err := c.config.KubeCli.Pods(ns).Get(oldPod.GetName(), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("fail to get pod %q: %v", kubernetesutil.ResourceKey(oldPod), err)
	}
	oldpod := pod.DeepCopy()

	c.logger.Infof("upgrading the NATS member %q from %s to %s", kubernetesutil.ResourceKey(oldPod), kubernetesutil.GetNATSVersion(pod), c.cluster.Spec.Version)
	pod.Spec.Containers[0].Image = kubernetesutil.MakeNATSImage(c.cluster.Spec.Version, c.cluster.Spec.ServerImage)
	kubernetesutil.SetNATSVersion(pod, c.cluster.Spec.Version)

	patchdata, err := kubernetesutil.CreatePatch(oldpod, pod, v1.Pod{})
	if err != nil {
		return fmt.Errorf("error creating patch: %v", err)
	}

	_, err = c.config.KubeCli.Pods(ns).Patch(pod.GetName(), types.StrategicMergePatchType, patchdata)
	if err != nil {
		return fmt.Errorf("fail to update the NATS member %q: %v", kubernetesutil.ResourceKey(oldPod), err)
	}

	// Wait for the pod to be running and ready.
	c.logger.Infof("waiting for pod %q to become ready", kubernetesutil.ResourceKey(pod))
	ctx, fn := context.WithTimeout(context.Background(), podReadinessTimeout)
	defer fn()
	if err := kubernetesutil.WaitUntilPodReady(ctx, c.config.KubeCli, pod); err != nil {
		return err
	}
	c.logger.Infof("pod %q became ready", kubernetesutil.ResourceKey(pod))

	c.logger.Infof("finished upgrading the NATS member %q", kubernetesutil.ResourceKey(pod))
	return nil
}

// upgradeTerminatedPod upgrades the version of a pod for which one of the containers has already terminated.
// It does this by deleting and re-creating the pod.
func (c *Cluster) upgradeTerminatedPod(pod *v1.Pod) error {
	if err := c.deletePod(pod); err != nil {
		return err
	}
	if _, err := c.createPod(); err != nil {
		return err
	}
	return nil
}

func (c *Cluster) maybeUpgradeMgmtService() error {
	ns := c.cluster.Namespace
	sn := kubernetesutil.ManagementServiceName(c.cluster.Name)

	svc, err := c.config.KubeCli.Services(ns).Get(sn, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get service \"%s/%s\": %v", ns, sn, err)
	}
	if svc.Spec.Selector[kubernetesutil.LabelClusterVersionKey] == c.cluster.Spec.Version {
		c.logger.Infof("NATS management service %q has already been updated to %s", kubernetesutil.ResourceKey(svc), c.cluster.Spec.Version)
		return nil
	}
	oldsvc := svc.DeepCopy()

	svc.Spec.Selector[kubernetesutil.LabelClusterVersionKey] = c.cluster.Spec.Version

	patchdata, err := kubernetesutil.CreatePatch(oldsvc, svc, v1.Service{})
	if err != nil {
		return fmt.Errorf("error creating patch: %v", err)
	}

	_, err = c.config.KubeCli.Services(ns).Patch(svc.GetName(), types.StrategicMergePatchType, patchdata)
	if err != nil {
		return fmt.Errorf("fail to update the NATS management service %q: %v", kubernetesutil.ResourceKey(svc), err)
	}
	c.logger.Infof("finished upgrading the NATS management service %q", kubernetesutil.ResourceKey(svc))
	return nil
}
