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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/util/slice"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	natsclient "github.com/nats-io/nats-operator/pkg/client/clientset/versioned"
	natsalphav2client "github.com/nats-io/nats-operator/pkg/client/clientset/versioned/typed/nats/v1alpha2"
	natslisters "github.com/nats-io/nats-operator/pkg/client/listers/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/debug"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	stringutil "github.com/nats-io/nats-operator/pkg/util/strings"
	"github.com/nats-io/nats-operator/pkg/util/versionCheck"
)

var (
	podTerminationGracePeriod = int64(5)
)

const (
	// defaultNatsLameDuckDuration is the default duration for the "lame duck" mode.
	// https://github.com/nats-io/gnatsd/blob/master/server/const.go#L136-L138
	defaultNatsLameDuckDuration = 2 * time.Minute
	// podDeletionTimeout is the maximum amount of time we wait for a pod to be actually deleted from the Kubernetes API after we delete it.
	podDeletionTimeout = 3 * time.Minute
	// podExecTimeout is the maximum amount of time we wait for an "exec" call to a container in a pod to produce a result.
	podExecTimeout = 10 * time.Second
	// podReadinessTimeout is the maximum amount of time we wait for a pod to be ready after we create/upgrade it.
	podReadinessTimeout = 5 * time.Minute
)

type Config struct {
	// Deprecated: Use KubeClient and its CoreV1() function instead.
	KubeCli corev1client.CoreV1Interface
	// Deprecated: Use NatsClient and its NatsV1alpha2() function instead.
	OperatorCli           natsalphav2client.NatsV1alpha2Interface
	PodLister             corev1listers.PodLister
	SecretLister          corev1listers.SecretLister
	ServiceLister         corev1listers.ServiceLister
	NatsServiceRoleLister natslisters.NatsServiceRoleLister

	KubeClient kubernetes.Interface
	KubeConfig *rest.Config
	NatsClient natsclient.Interface
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
		logger:          logrus.WithField("pkg", "cluster").WithField("namespace", cl.Namespace).WithField("cluster-name", cl.Name),
		debugLogger:     debug.New(cl.Namespace, cl.Name),
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

	// Poll pods in order to understand which are pending and which must be deleted.
	_, waiting, deletable, err := c.pollPods()
	if err != nil {
		reconcileFailed.WithLabelValues("failed to poll pods").Inc()
		return fmt.Errorf("failed to poll pods: %v", err)
	}

	// Delete all pods in terminal phases.
	for _, pod := range deletable {
		c.logger.Warnf("deleting pod %q in terminal phase %q", kubernetesutil.ResourceKey(pod), pod.Status.Phase)
		if err := c.deletePod(pod); err != nil {
			c.logger.Errorf("failed to delete pod %q: %v", kubernetesutil.ResourceKey(pod), err)
			return err
		}
	}

	// If there are pods in "waiting" state, exit cleanly and wait for the next reconcile iteration (which will happen as soon as these pods become running/succeeded/failed).
	// This should not happen often in practice, as createPod waits for pods to be running before returning, but it is still a good safety measure.
	if len(waiting) > 0 {
		c.logger.Infof("skipping reconciliation as there are %d waiting pods (%v)", len(waiting), kubernetesutil.GetPodNames(waiting))
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
	// Grab the list of currently running pods.
	// It is acceptable to call pollPods every time we create a new pod as it uses the pod lister (instead of hitting the Kubernetes API directly).
	pods, _, _, err := c.pollPods()
	if err != nil {
		return nil, err
	}

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
	pod := kubernetesutil.NewNatsPodSpec(c.cluster.Namespace, name, c.cluster.Name, c.cluster.Spec, c.cluster.AsOwner())
	pod, err = c.config.KubeCli.Pods(c.cluster.Namespace).Create(pod)
	if err != nil {
		return nil, err
	}

	// Wait for the pod to be running and ready.
	c.logger.Infof("waiting for pod %q to become ready", kubernetesutil.ResourceKey(pod))
	ctx, fn := context.WithTimeout(context.Background(), podReadinessTimeout)
	defer fn()
	if err := kubernetesutil.WaitUntilPodReady(ctx, c.config.KubeCli, pod); err != nil {
		return nil, err
	}
	c.logger.Infof("pod %q became ready", kubernetesutil.ResourceKey(pod))

	// Append the newly created pod to the slice of existing pods.
	pods = append(pods, pod)
	return pod, err
}

// tryGracefulPodDeletion attempts to gracefully remove the specified pod from the NATS cluster.
// It does this by trying to make the "gnatsd" process enter the "lame duck" mode before actually attempting to delete the pod.
// This is done in a best-effort basis, since the NATS version running in the pod may not support this mode.
// For that reason, we just log any errors without actually failing and proceed to the actual deletion of the pod.
func (c *Cluster) tryGracefulPodDeletion(pod *v1.Pod) error {
	if err := c.enterLameDuckModeAndWaitTermination(pod); err != nil {
		c.logger.Warn(err)
	}
	return c.deletePod(pod)
}

// deletePod removes the specified pod from the current NATS cluster.
// This function DOES NOT attempt to gracefully shutdown the "gnatsd" process.
// Hence, it should only be used after having tried a gracefully shutdown.
func (c *Cluster) deletePod(pod *v1.Pod) error {
	// Establish a watch on the pod that will notify us when the pod is deleted.
	// It may happen that deletion takes effect too soon after we make the call to "Delete()" (e.g. in cases when the pod has entered the "lame duck" mode and is already in the "Succeeded" phase).
	// In these scenarios, we may easily lose the "DELETED" event since we haven't had the opportunity to establish the watch yet.
	// Hence, we must establish the watch beforehand in a separate goroutine.
	// We must also wait for the informer to acknowledge the pod (i.e. through an "ADDED" event) before proceeding to the actual deletion.
	ackCh := make(chan struct{})
	errCh := make(chan error)
	go func() {
		ctx, fn := context.WithTimeout(context.Background(), podDeletionTimeout)
		defer fn()
		err := kubernetesutil.WaitUntilPodCondition(ctx, c.config.KubeCli, pod, func(event watch.Event) (bool, error) {
			switch event.Type {
			case watch.Added:
				// Signal that the informer has acknowledged the pod and keep watching.
				close(ackCh)
				return false, nil
			case watch.Error:
				// Abort watching and propagate an error.
				return false, fmt.Errorf("got event of type %q for pod %q", event.Type, kubernetesutil.ResourceKey(pod))
			default:
				// Only stop watching if the event type is "DELETED".
				return event.Type == watch.Deleted, nil
			}
		})
		if err != nil {
			errCh <- err
		} else {
			close(errCh)
		}
	}()
	// Wait for the informer to acknowledge the pod before proceeding to the actual deletion.
	<-ackCh

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
	if err := <-errCh; err != nil {
		return err
	}
	if c.isDebugLoggerEnabled() {
		c.debugLogger.LogPodDeletion(pod)
	}
	return nil
}

// pollPods lists pods belonging to the current NATS cluster, and returns the list of running and pending pods.
func (c *Cluster) pollPods() (running []*v1.Pod, waiting []*v1.Pod, deletable []*v1.Pod, err error) {
	// List existing pods belonging to the current NATS cluster.
	pods, err := c.config.PodLister.Pods(c.cluster.Namespace).List(kubernetesutil.LabelSelectorForCluster(c.cluster.Name))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list running pods: %v", err)
	}

	// Iterate over pods in order to obtain a list of running, "waiting" and deletable ones.
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
		// Add the current pod to the appropriate slice based on the current phase.
		switch pod.Status.Phase {
		case v1.PodRunning:
			running = append(running, pod)
		case v1.PodPending:
			fallthrough
		case v1.PodUnknown:
			waiting = append(waiting, pod)
		case v1.PodSucceeded:
			fallthrough
		case v1.PodFailed:
			deletable = append(deletable, pod)
		}
	}

	// Sort pods by their name in each slice order to keep existing scale up/down behavior intact.
	sort.Slice(running, func(i, j int) bool {
		return running[i].Name < running[j].Name
	})
	sort.Slice(waiting, func(i, j int) bool {
		return waiting[i].Name < waiting[j].Name
	})
	sort.Slice(deletable, func(i, j int) bool {
		return deletable[i].Name < deletable[j].Name
	})

	// Return the computed slices.
	return running, waiting, deletable, nil
}

// updateCluster patches the current NatsCluster resource in order for it to reflect the current state.
func (c *Cluster) updateCluster() error {
	// Apply idempotent update to the server configuration,
	// which may cause a reload if config has changed.
	err := c.updateConfigSecret()
	if err != nil {
		c.logger.Errorf("failed to update cluster secret: %v", err)
	}

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

// enterLameDuckModeAndWaitTermination execs into the "nats" container of the specified pod and attempts to send the "ldm" signal to the "gnatsd" process.
// In case this succeeds, the funcion blocks until the "nats" container reaches the "Terminated" state (indicating that the "lame duck" mode has been entered and NATS is ready to shutdown) or until a timeout is reached.
// Otherwise, it returns an error which should be handled by the caller.
func (c *Cluster) enterLameDuckModeAndWaitTermination(pod *v1.Pod) error {
	// Try to place NATS in "lame duck" mode by sending the process the "ldm" signal.
	// We wait for at most "podExecTimeout" for the "exec" command to return a result.
	ctx, fn := context.WithTimeout(context.Background(), podExecTimeout)
	defer fn()

	version := kubernetesutil.GetNATSVersion(pod)

	args := []string{
		versionCheck.ServerBinaryPath(version),
		"-sl",
		fmt.Sprintf("ldm=%s", constants.PidFilePath),
	}
	exitCode, err := kubernetesutil.ExecInContainer(ctx, c.config.KubeClient, c.config.KubeConfig, pod.Namespace, pod.Name, constants.NatsContainerName, args...)
	if exitCode == 0 || err == context.DeadlineExceeded {
		// At this point, we were either explicitly successful at placing the NATS instance in "lame duck" mode, or the "exec" command has timed out and we don't know its result.
		// In the latter case, it may still be possible that the NATS instance has been placed in "lame duck" mode.
		// Hence, we should wait for the pod to reach the "Terminated" state in both scenarios.
		return c.waitNatsContainerTermination(pod)
	}
	return fmt.Errorf("failed to place nats in \"lame duck\" mode: %v", err)
}

// waitNatsContainerTermination blocks until the "nats" container of the specified pod reaches the "Terminated" state, or until twice the value of ".spec.lameDuckDuration".
func (c *Cluster) waitNatsContainerTermination(pod *v1.Pod) error {
	// If no value for the duration of the "lame duck" mode has been specified, we use the default.
	var ldDuration int64
	if c.cluster.Spec.LameDuckDurationSeconds == nil {
		ldDuration = int64(defaultNatsLameDuckDuration.Seconds())
	} else {
		ldDuration = *c.cluster.Spec.LameDuckDurationSeconds
	}
	// Wait for at most twice the specified (or default) duration for the "lame duck" mode, so that we don't fail too early but also don't wait for too long.
	ctx, fn := context.WithTimeout(context.Background(), time.Duration(2*ldDuration)*time.Second)
	defer fn()
	return kubernetesutil.WaitUntilPodCondition(ctx, c.config.KubeClient.CoreV1(), pod, func(event watch.Event) (bool, error) {
		pod := event.Object.(*v1.Pod)
		switch event.Type {
		case watch.Deleted:
			fallthrough
		case watch.Error:
			return false, fmt.Errorf("got event of unexpected type %q for pod %q while waiting termination", event.Type, kubernetesutil.ResourceKey(pod))
		default:
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if containerStatus.Name == constants.NatsContainerName {
					if containerStatus.State.Terminated != nil {
						return true, nil
					}
				}
			}
			return false, nil
		}
	})
}
