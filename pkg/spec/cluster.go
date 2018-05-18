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

package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats-operator/pkg/constants"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NatsClusterList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: http://releases.k8s.io/HEAD/docs/devel/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NatsCluster `json:"items"`
}

// NatsCluster is a NATS cluster.
//
// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NatsCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ClusterSpec   `json:"spec"`
	Status            ClusterStatus `json:"status"`
}

func (c *NatsCluster) AsOwner() metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: c.APIVersion,
		Kind:       c.Kind,
		Name:       c.Name,
		UID:        c.UID,
		Controller: &trueVar,
	}
}

type ClusterSpec struct {
	// Size is the expected positive size of the NATS cluster.
	// The operator will eventually make the size of the running
	// cluster equal to the expected size.
	Size int `json:"size"`

	// Version is the expected version of the NATS cluster.
	// The operator will eventually make the cluster version
	// equal to the expected version.
	//
	// The version must follow the [semver]( http://semver.org) format, for example "1.0.4".
	// Only NATS released versions are supported: https://github.com/nats-io/gnatsd/releases
	//
	Version string `json:"version"`

	ExporterVersion string `json:"exporterVersion"`

	// Paused is to pause the control of the operator for the cluster.
	Paused bool `json:"paused,omitempty"`

	// Pod defines the policy to create pod for the NATS pod.
	//
	// Updating Pod does not take effect on any existing NATS pods.
	Pod *PodPolicy `json:"pod,omitempty"`

	// TLS is the configuration to secure the cluster.
	TLS *TLSConfig `json:"tls,omitempty"`

	// Auth is the configuration to set permissions for users.
	Auth *AuthConfig `json:"auth,omitempty"`
}

// TLSConfig is the optional TLS configuration for the cluster.
type TLSConfig struct {
	// ServerSecret is the secret containing the certificates
	// to secure the port to which the clients connect.
	ServerSecret string `json:"serverSecret,omitempty"`

	// RoutesSecret is the secret containing the certificates
	// to secure the port to which cluster routes connect.
	RoutesSecret string `json:"routesSecret,omitempty"`
}

// PodPolicy defines the policy to create pod for the NATS container.
type PodPolicy struct {
	// Labels specifies the labels to attach to pods the operator creates for the
	// NATS cluster.
	// "app" and "nats_*" labels are reserved for the internal use of this operator.
	// Do not overwrite them.
	Labels map[string]string `json:"labels,omitempty"`

	// NodeSelector specifies a map of key-value pairs. For the pod to be eligible
	// to run on a node, the node must have each of the indicated key-value pairs as
	// labels.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// AntiAffinity determines if the nats-operator tries to avoid putting
	// the NATS members in the same cluster onto the same node.
	AntiAffinity bool `json:"antiAffinity,omitempty"`

	// Resources is the resource requirements for the NATS container.
	// This field cannot be updated once the cluster is created.
	Resources v1.ResourceRequirements `json:"resources,omitempty"`

	// Tolerations specifies the pod's tolerations.
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// List of environment variables to set in the NATS container.
	NatsEnv []v1.EnvVar `json:"natsEnv,omitempty"`

	// EnableConfigReload attaches a sidecar to each NATS Server
	// that will signal the server whenever the configuration is updated.
	EnableConfigReload bool `json:"enableConfigReload,omitempty"`

	// ReloaderImage is the image to use for the reloader.
	ReloaderImage string `json:"reloaderImage,omitempty"`

	// ReloaderImageTag is the tag of the reloader image.
	ReloaderImageTag string `json:"reloaderImageTag,omitempty"`

	// ReloaderImagePullPolicy is the pull policy for the reloader image.
	ReloaderImagePullPolicy string `json:"reloaderImagePullPolicy,omitempty"`
}

// AuthConfig is the authorization configuration for
// user permissions in the cluster.
type AuthConfig struct {
	// EnableServiceAccounts makes the operator lookup for mappings among
	// Kubernetes ServiceAccounts and NatsServiceRoles to issue tokens that
	// can be used to authenticate against a NATS cluster with authorization
	// following the permissions set for the role.
	EnableServiceAccounts bool `json:"enableServiceAccounts,omitempty"`

	// ClientsAuthSecret is the secret containing the explicit authorization
	// configuration in JSON.
	ClientsAuthSecret string `json:"clientsAuthSecret,omitempty"`

	// ClientsAuthTimeout is the time in seconds that the NATS server will
	// allow to clients to send their auth credentials.
	ClientsAuthTimeout int `json:"clientsAuthTimeout,omitempty"`
}

func (c *ClusterSpec) Validate() error {
	if c.Pod != nil {
		for k := range c.Pod.Labels {
			if k == "app" || strings.HasPrefix(k, "nats_") {
				return errors.New("spec: pod labels contains reserved label")
			}
		}
	}
	return nil
}

// Cleanup cleans up user passed spec, e.g. defaulting, transforming fields.
// TODO: move this to admission controller
func (c *ClusterSpec) Cleanup() {
	if len(c.Version) == 0 {
		c.Version = constants.DefaultNatsVersion
	}

	c.Version = strings.TrimLeft(c.Version, "v")
}

type ClusterPhase string

const (
	ClusterPhaseNone     ClusterPhase = ""
	ClusterPhaseCreating              = "Creating"
	ClusterPhaseRunning               = "Running"
	ClusterPhaseFailed                = "Failed"
)

type ClusterCondition struct {
	Type ClusterConditionType `json:"type"`

	Reason string `json:"reason"`

	TransitionTime string `json:"transitionTime"`
}

type ClusterConditionType string

const (
	ClusterConditionReady = "Ready"

	ClusterConditionScalingUp   = "ScalingUp"
	ClusterConditionScalingDown = "ScalingDown"

	ClusterConditionUpgrading = "Upgrading"
)

type ClusterStatus struct {
	// Phase is the cluster running phase
	Phase  ClusterPhase `json:"phase"`
	Reason string       `json:"reason"`

	// ControlPaused indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Condition keeps ten most recent cluster conditions
	Conditions []ClusterCondition `json:"conditions"`

	// Size is the current size of the cluster
	Size int `json:"size"`
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`
}

func (cs ClusterStatus) Copy() ClusterStatus {
	newCS := ClusterStatus{}
	b, err := json.Marshal(cs)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(b, &newCS)
	if err != nil {
		panic(err)
	}
	return newCS
}

func (cs *ClusterStatus) IsFailed() bool {
	if cs == nil {
		return false
	}
	return cs.Phase == ClusterPhaseFailed
}

func (cs *ClusterStatus) SetPhase(p ClusterPhase) {
	cs.Phase = p
}

func (cs *ClusterStatus) PauseControl() {
	cs.ControlPaused = true
}

func (cs *ClusterStatus) Control() {
	cs.ControlPaused = false
}

func (cs *ClusterStatus) SetCurrentVersion(v string) {
	cs.CurrentVersion = v
}

func (cs *ClusterStatus) SetReason(r string) {
	cs.Reason = r
}

func (cs *ClusterStatus) AppendScalingUpCondition(from, to int) {
	c := ClusterCondition{
		Type:           ClusterConditionScalingUp,
		Reason:         scalingReason(from, to),
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) AppendScalingDownCondition(from, to int) {
	c := ClusterCondition{
		Type:           ClusterConditionScalingDown,
		Reason:         scalingReason(from, to),
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) AppendUpgradingCondition(to string, member string) {
	reason := fmt.Sprintf("upgrading cluster member %s version to %v", member, to)

	c := ClusterCondition{
		Type:           ClusterConditionUpgrading,
		Reason:         reason,
		TransitionTime: time.Now().Format(time.RFC3339),
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) SetReadyCondition() {
	c := ClusterCondition{
		Type:           ClusterConditionReady,
		Reason:         "Current cluster state matches target state.",
		TransitionTime: time.Now().Format(time.RFC3339),
	}

	if len(cs.Conditions) == 0 {
		cs.appendCondition(c)
		return
	}

	lastc := cs.Conditions[len(cs.Conditions)-1]
	if lastc.Type == ClusterConditionReady {
		return
	}
	cs.appendCondition(c)
}

func (cs *ClusterStatus) appendCondition(c ClusterCondition) {
	cs.Conditions = append(cs.Conditions, c)
	if len(cs.Conditions) > 10 {
		cs.Conditions = cs.Conditions[1:]
	}
}

func scalingReason(from, to int) string {
	return fmt.Sprintf("Current cluster size: %d, desired cluster size: %d", from, to)
}
