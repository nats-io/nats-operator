// +build e2e

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

package e2e

import (
	"context"
	"testing"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/features"
)

// TestCreateClusterInDedicatedNamespace creates a NatsCluster resource in a dedicated namespace and waits for the full mesh to be formed.
func TestCreateClusterInDedicatedNamespace(t *testing.T) {
	if !f.FeatureMap.IsEnabled(features.ClusterScoped) {
		t.Skip("skipping as the current deployment of nats-operator is not cluster-scoped")
	}

	var (
		size    = 3
		version = "1.4.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a namespace for the NATS cluster.
	n, err := f.CreateNamespace()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err = f.DeleteNamespace(n); err != nil {
			t.Error(err)
		}
	}()

	// Create a NatsCluster resource with three members.
	if natsCluster, err = f.CreateCluster(n.Name, "test-nats-", size, version); err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)
		}
	}()

	// Wait until the full mesh is formed.
	ctx, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx, natsCluster, size, version); err != nil {
		t.Fatal(err)
	}
}
