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

	"github.com/pires/nats-operator/pkg/util/k8sutil"
)

func (c *Cluster) upgradePeer(podName string) error {
	pod, err := c.kclient.Pods(c.namespace).Get(podName)
	if err != nil {
		return fmt.Errorf("fail to get pod (%s): %v", podName, err)
	}
	c.logger.Infof("upgrading the NATS peer %v from %s to %s", podName, k8sutil.GetNATSVersion(pod), c.spec.Version)
	pod.Spec.Containers[0].Image = k8sutil.MakeNATSImage(c.spec.Version)
	k8sutil.SetNATSVersion(pod, c.spec.Version)
	_, err = c.kclient.Pods(c.namespace).Update(pod)
	if err != nil {
		return fmt.Errorf("fail to update the NATS peer (%s): %v", podName, err)
	}
	c.logger.Infof("finished upgrading the NATS peer %v", podName)
	return nil
}
