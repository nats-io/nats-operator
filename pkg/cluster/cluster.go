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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/kubernetes/pkg/util/slice"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	natsalphav2client "github.com/nats-io/nats-operator/pkg/client/clientset/versioned/typed/nats/v1alpha2"
	natslisters "github.com/nats-io/nats-operator/pkg/client/listers/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/debug"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	stringutil "github.com/nats-io/nats-operator/pkg/util/strings"
)

var (
	podTerminationGracePeriod = int64(5)
)

type Config struct {
	KubeCli               corev1client.CoreV1Interface
	OperatorCli           natsalphav2client.NatsV1alpha2Interface
	PodLister             corev1listers.PodLister
	SecretLister          corev1listers.SecretLister
	ServiceLister         corev1listers.ServiceLister
	NatsServiceRoleLister natslisters.NatsServiceRoleLister
}

type Cluster struct {
	// logger is the logger to use during the current iteration.
	logger *logrus.Entry
	// debugLogger is the debug logger to use during the current iteration.
	debugLogger *debug.DebugLogger
	// config holds the clients and listers required for the reconciler to operate.
	config Config
	// cluster holds the NatsCluster resource which we are going to reconcile.
	cluster *v1alpha2.NatsCluster
	// originalCluster holds the original, unmodified NatsCluster resource.
	// Used to create a patch in the end of the reconcile loop.
	originalCluster *v1alpha2.NatsCluster
}

// New returns a new instance of the reconciler for NatsCluster resources.
func New(config Config, cl *v1alpha2.NatsCluster) *Cluster {
	return &Cluster{
		logger:          logrus.WithField("pkg", "cluster").WithField("cluster-name", cl.Name),
		debugLogger:     debug.New(cl.Name),
		config:          config,
		cluster:         cl,
		originalCluster: cl.DeepCopy(),
	}
}

// Reconcile looks at the current state of the associated NatsCluster resource and attempts to drive it towards the desired state.
func (c *Cluster) Reconcile() error {
	// Exit immediately in case the NatsCluster resource is marked as paused.
	if c.cluster.Spec.Paused {
		c.logger.Infof("control is paused, skipping reconciliation")
		c.cluster.Status.PauseControl()
		return c.updateCluster()
	}

	// Mark the NatsCluster resource as being active.
	c.cluster.Status.Control()

	// Take note of the current time so we can later report the duration of the current iteration.
	start := time.Now()

	// Make sure that both the client and management services for the current cluster have been created.
	if err := c.checkServices(); err != nil {
		return fmt.Errorf("failed to create services: %v", err)
	}

	// Make sure that the configuration secret for the current cluster has been created.
	if err := c.checkConfigSecret(); err != nil {
		return fmt.Errorf("failed to create config secret: %s", err)
	}

	// If the current NatsCluster resource has authentication configured, make sure that the configuration is in sync with the secrets.
	if c.cluster.Spec.Auth != nil {
		err := c.checkClientAuthUpdate()
		if err != nil {
			return fmt.Errorf("failed to update auth data in config secret: %v", err)
		}
	}

	// Poll pods in order to understand which are running and which are pending.
	_, pending, err := c.pollPods()
	if err != nil {
		reconcileFailed.WithLabelValues("failed to poll pods").Inc()
		return fmt.Errorf("failed to poll pods: %v", err)
	}

	// If there are pods in pending state, exit cleanly and wait for the next reconcile iteration (which will happen as soon as these pods become running/succeeded/failed).
	// This should not happen often in practice, as createPod waits for pods to be running before returning, but it is still a good safety measure.
	if len(pending) > 0 {
		c.logger.Infof("skipping reconciliation as there are %d pending pods (%v)", len(pending), kubernetesutil.GetPodNames(pending))
		return nil
	}

	// Reconcile the size and version of the cluster.
	if err := c.checkPods(); err != nil {
		reconcileFailed.WithLabelValues("failed to reconcile pods").Inc()
		return fmt.Errorf("failed to reconcile pods: %v", err)
	}

	// Mark the cluster as ready.
	c.cluster.Status.SetReadyCondition()

	// Update the status of the NatsCluster resource.
	if err := c.updateCluster(); err != nil {
		reconcileFailed.WithLabelValues("failed to update status").Inc()
		return fmt.Errorf("failed to update status: %v", err)
	}

	// Take note of the time it took to perform the current iteration and return.
	reconcileHistogram.WithLabelValues(c.name()).Observe(time.Since(start).Seconds())
	return nil
}

func (c *Cluster) checkClientAuthUpdate() error {
	if c.cluster.Spec.Auth.ClientsAuthSecret != "" {
		// Look for updates in the secret used for auth and trigger a config reload in case there are new updates.
		// The resource version of the secret is stored in an annotation in the NatsCluster resource.
		result, err := c.config.SecretLister.Secrets(c.cluster.Namespace).Get(c.cluster.Spec.Auth.ClientsAuthSecret)
		if err != nil {
			return err
		}
		if c.cluster.GetClientAuthSecretResourceVersion() != result.ResourceVersion {
			c.cluster.SetClientAuthSecretResourceVersion(result.ResourceVersion)
			return c.updateConfigSecret()
		}
	} else if c.cluster.Spec.Auth.EnableServiceAccounts {
		// Get the hash of the comma-separated list of NatsServiceRole UIDs associated with the current NATS cluster.
		currentHash := c.cluster.GetNatsServiceRolesHash()

		// Lookup for service roles that may have been created.
		// TODO(wallyqs): Should also get secrets in case they were deleted
		// so that they get recreated in case of manual deletion, this would
		// enable revoking on the fly the tokens since reload would override
		// the previous credentials.
		roles, err := c.config.NatsServiceRoleLister.NatsServiceRoles(c.cluster.Namespace).List(kubernetesutil.NatsServiceRoleLabelSelectorForCluster(c.cluster.Name))
		if err != nil {
			return err
		}

		// Sort NatsServiceRole resources by their UID so we can get predictable results.
		sort.Slice(roles, func(i, j int) bool {
			return roles[i].UID < roles[j].UID
		})

		// Compute the hash of the comma-separated list of NatsServiceRole UIDs and corresponding resource versions targeting the current NATS cluster.
		desiredUIDs := make([]string, len(roles))
		for _, role := range roles {
			desiredUIDs = append(desiredUIDs, fmt.Sprintf("%s:%s", role.UID, role.ResourceVersion))
		}
		desiredHash := stringutil.HashSlice(desiredUIDs)

		// Update the configuration if the hashes differ.
		if currentHash != desiredHash {
			c.cluster.SetNatsServiceRolesHash(desiredHash)
			return c.updateConfigSecret()
		}
	}
	return nil
}

// checkServices makes sure that the client and management services exist.
func (c *Cluster) checkServices() error {
	var (
		// mustCreateClientService indicates whether we must create the client service.
		mustCreateClientService bool
		// mustCreateManagementService indicates whether we must create the management service.
		mustCreateManagementService bool
	)

	// Check whether the client service already exists.
	if _, err := c.config.ServiceLister.Services(c.cluster.Namespace).Get(kubernetesutil.ClientServiceName(c.cluster.Name)); err != nil {
		if kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			// The client service does not exist, so we must create it.
			mustCreateClientService = true
		} else {
			// We've got an unexpected error while getting the service.
			return err
		}
	}

	// Create the client service if required.
	if mustCreateClientService {
		if err := kubernetesutil.CreateClientService(c.config.KubeCli, c.cluster.Name, c.cluster.Namespace, c.cluster.AsOwner()); err != nil {
			return err
		}
	}

	// Check whether we need to create the management service.
	if _, err := c.config.ServiceLister.Services(c.cluster.Namespace).Get(kubernetesutil.ManagementServiceName(c.cluster.Name)); err != nil {
		if kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			// The management service does not exist, so we must create it.
			mustCreateManagementService = true
		} else {
			// We've got an unexpected error while getting the service.
			return err
		}
	}

	// Create the management service if required.
	if mustCreateManagementService {
		if err := kubernetesutil.CreateMgmtService(c.config.KubeCli, c.cluster.Name, c.cluster.Spec.Version, c.cluster.Namespace, c.cluster.AsOwner()); err != nil {
			return err
		}
	}

	return nil
}

// checkConfigSecret makes sure that the secret used to hold the configuration for the current NATS cluster exists.
func (c *Cluster) checkConfigSecret() error {
	var (
		// mustCreateSecret indicates whether we must create the configuration secret.
		mustCreateSecret bool
	)

	// Check whether the secret backing up the configuration already exists.
	if _, err := c.config.SecretLister.Secrets(c.cluster.Namespace).Get(kubernetesutil.ConfigSecret(c.cluster.Name)); err != nil {
		if kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			// The configmap does not exist, so we must create it.
			mustCreateSecret = true
		} else {
			// We've got an unexpected error while getting the configmap.
			return err
		}
	}

	// Create the secret if required.
	if mustCreateSecret {
		return kubernetesutil.CreateConfigSecret(c.config.KubeCli, c.config.OperatorCli, c.cluster.Name, c.cluster.Namespace, c.cluster.Spec, c.cluster.AsOwner())
	}

	return nil
}

func (c *Cluster) updateConfigSecret() error {
	return kubernetesutil.UpdateConfigSecret(c.config.KubeCli, c.config.OperatorCli, c.cluster.Name, c.cluster.Namespace, c.cluster.Spec, c.cluster.AsOwner())
}

// createPod creates a pod using the first available name.
// Pod names are of the form "<natscluster-name>-<idx>", where "<idx>" is a base-16 integer.
func (c *Cluster) createPod() (*v1.Pod, error) {
	// Grab the list of currently running and pending pods.
	// It is acceptable to call pollPods everytime we create a new pod as it uses the pod lister (instead of hitting the Kubernetes API directly).
	running, pending, err := c.pollPods()
	if err != nil {
		return nil, err
	}
	pods := append(running, pending...)

	// Grab a slice containing all existing pod names.
	podNames := make([]string, len(pods))
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}

	var (
		name string
	)

	// Use the first name not found in the podNames slice.
	for i := 1; i <= c.cluster.Spec.Size; i++ {
		name = fmt.Sprintf("%s-%x", c.cluster.Name, i)
		if !slice.ContainsString(podNames, name, nil) {
			break
		}
	}

	// Create the pod.
	pod := kubernetesutil.NewNatsPodSpec(name, c.cluster.Name, c.cluster.Spec, c.cluster.AsOwner())
	pod, err = c.config.KubeCli.Pods(c.cluster.Namespace).Create(pod)
	if err != nil {
		return nil, err
	}

	// Wait for the pod to be running and ready.
	c.logger.Infof("waiting for pod %q to become ready", pod.Name)
	if err := kubernetesutil.WaitUntilPodReady(c.config.KubeCli, pod); err != nil {
		return nil, err
	}
	c.logger.Infof("pod %q became ready", pod.Name)

	// Append the newly created pod to the slice of existing pods.
	pods = append(pods, pod)
	return pod, err
}

// removePod removes the specified pod from the current NATS cluster.
func (c *Cluster) removePod(pod *v1.Pod) error {
	// Delete the specified pod.
	err := c.config.KubeCli.Pods(pod.Namespace).Delete(pod.Name, metav1.NewDeleteOptions(podTerminationGracePeriod))
	if err != nil {
		if !kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			return err
		}
		if c.isDebugLoggerEnabled() {
			c.debugLogger.LogMessage(fmt.Sprintf("pod %q not found while trying to delete it", pod.Name))
		}
		// Exit in order to avoid waiting for a pod that doesn't exist anymore.
		return nil
	}
	// Wait until the specified pod has been deleted.
	err = kubernetesutil.WaitUntilPodCondition(c.config.KubeCli, pod, func(event watch.Event) (bool, error) {
		return event.Type == watch.Deleted, nil
	})
	if err != nil {
		return err
	}
	if c.isDebugLoggerEnabled() {
		c.debugLogger.LogPodDeletion(pod.Name)
	}
	return nil
}

// pollPods lists pods belonging to the current NATS cluster, and returns the list of running and pending pods.
func (c *Cluster) pollPods() (running, pending []*v1.Pod, err error) {
	// List existing pods belonging to the current NATS cluster.
	pods, err := c.config.PodLister.Pods(c.cluster.Namespace).List(kubernetesutil.LabelSelectorForCluster(c.cluster.Name))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list running pods: %v", err)
	}

	// Iterate over pods in order to obtain a list of running and pending ones.
	for _, pod := range pods {
		// Ignore pods without owner references.
		if len(pod.OwnerReferences) < 1 {
			c.logger.Warningf("ignoring orphaned pod %q", pod.Name)
			continue
		}
		// Ignore pods with invalid owner references.
		if pod.OwnerReferences[0].UID != c.cluster.UID {
			c.logger.Warningf("ignoring pod %q with unexpected owner %q", pod.Name, pod.OwnerReferences[0].UID)
			continue
		}
		// Compute the list of running and pending pods.
		// TODO: Take into account failed pods, so they can be handled as well, or use an adequate restart policy when creating pods.
		switch pod.Status.Phase {
		case v1.PodRunning:
			running = append(running, pod)
		case v1.PodPending:
			pending = append(pending, pod)
		default:
			c.logger.Warningf("ignoring pod %q with unexpected phase %q", pod.Name, pod.Status.Phase)
		}
	}

	// Sort running and pending pods by their name in order to keep existing scale up/down behavior intact.
	sort.Slice(running, func(i, j int) bool {
		return running[i].Name < running[j].Name
	})
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Name < pending[j].Name
	})

	// Return the computed lists of running and pending pods.
	return running, pending, nil
}

// updateCluster patches the current NatsCluster resource in order for it to reflect the current state.
func (c *Cluster) updateCluster() error {
	// Return if there are no changes.
	if reflect.DeepEqual(c.originalCluster, c.cluster) {
		return nil
	}

	patchBytes, err := kubernetesutil.CreatePatch(c.originalCluster, c.cluster, &v1alpha2.NatsCluster{})
	if err != nil {
		return err
	}

	// Patch the NatsCluster resource.
	_, err = c.config.OperatorCli.NatsClusters(c.cluster.Namespace).Patch(c.cluster.Name, types.MergePatchType, patchBytes)
	if err != nil {
		return fmt.Errorf("failed to update status: %v", err)
	}
	return nil
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

func (c *Cluster) isDebugLoggerEnabled() bool {
	if c.debugLogger != nil {
		return true
	}
	return false
}
