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

package spec

import (
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
)

// TODO: supports object store like s3
type StorageType string

const (
	BackupStorageTypePersistentVolume = "PersistentVolume"
)

type NatsCluster struct {
	unversioned.TypeMeta `json:",inline"`
	api.ObjectMeta       `json:"metadata,omitempty"`
	Spec                 ClusterSpec `json:"spec"`
}

type ClusterSpec struct {
	// Size is the expected positive size of the NATS cluster.
	// The operator will eventually make the size of the running
	// cluster equal to the expected size.
	Size int `json:"size"`

	// Version is the expected version of the NATS cluster.
	// The operator will eventually make the cluster version
	// equal to the expected version.
	Version string `json:"version"`

	// StorageType specifies the type of storage device to store files.
	// If it's not set by user, the default is "PersistentVolume".
	StorageType StorageType `json:"storageType"`

	// Paused is to pause the control of the operator for the cluster.
	Paused bool `json:"paused,omitempty"`

	// NodeSelector specifies a map of key-value pairs. For the pod to be eligible
	// to run on a node, the node must have each of the indicated key-value pairs as
	// labels.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// AntiAffinity determines if the operator tries to avoid scheduling
	// NATS pods related to a same cluster onto the same node.
	AntiAffinity bool `json:"antiAffinity"`
}
