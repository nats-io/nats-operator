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
)

// TestUpgradeCluster creates a NatsCluster resource with version
// 1.3.0 and waits for the full mesh to be formed.  Then, it updates
// the ".spec.version" field of the NatsCluster resource to 1.4.0 and
// waits for the upgrade to be performed.
func TestUpgradeCluster(t *testing.T) {
	var (
		initialVersion = "1.3.0"
		finalVersion   = "1.4.0"
		size           = 2
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with three members.
	if natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-upgrade-", size, initialVersion); err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)
		}
	}()

	// Wait until the full mesh is formed with the initial version.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx1, natsCluster, size, initialVersion); err != nil {
		t.Fatal(err)
	}

	natsCluster.Spec.Version = finalVersion
	if natsCluster, err = f.PatchCluster(natsCluster); err != nil {
		t.Fatal(err)
	}

	// Wait until the full mesh is formed with the final version.
	ctx2, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx2, natsCluster, size, finalVersion); err != nil {
		t.Fatal(err)
	}
}
