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
	"k8s.io/api/core/v1"
	"testing"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/util/context"
)

// TestConfigContainsFullMesh creates a NatsCluster resource with size 3 and waits for the full mesh to be formed.
// Then, it waits until the configuration secret has references to all pods belonging to the NatsCluster resource.
func TestConfigContainsFullMesh(t *testing.T) {
	var (
		size    = 3
		version = "1.3.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with three members.
	if natsCluster, err = f.CreateCluster("test-nats-", size, version); err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)
		}
	}()

	// Wait until the full mesh is formed.
	if err = f.WaitUntilFullMeshWithVersion(context.WithTimeout(waitTimeout), natsCluster, size, version); err != nil {
		t.Fatal(err)
	}

	// Make sure that the configuration file contains routes to all the pods.
	if err = f.WaitUntilExpectedRoutesInConfig(context.WithTimeout(waitTimeout), natsCluster); err != nil {
		t.Fatal(err)
	}
}

// TestExistingSecretIsReplaced creates a Secret containing a "nats.conf" entry with minimal configuration.
// Then, it creates a NatsCluster resource with the same name and waits for the "nats.conf" entry to be replaced with the appropriate config.
func TestExistingSecretIsReplaced(t *testing.T) {
	var (
		size    = 3
		version = "1.3.0"
	)

	var (
		secret      *v1.Secret
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a secret with the same name we will use to create the NatsCluster resource.
	if secret, err = f.CreateSecret(constants.ConfigFileName, []byte("{port:4222}")); err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the Secret resource after we're done testing.
	defer func() {
		if err = f.DeleteSecret(secret); err != nil {
			t.Error(err)
		}
	}()

	// Create a NatsCluster resource, making sure that its name matches the name of the secret we've created above.
	natsCluster, err = f.CreateCluster("", size, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		natsCluster.Name = secret.Name
	})
	if err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)
		}
	}()

	// We can't wait for the full mesh to be formed since the original configuration doesn't contain clustering configuration, and since configuration reloading is disabled.
	// Hence, we just wait for all the required routes to show up on the "nats.conf" entry.

	// Wait until the configuration file contains routes to all the pods (meaning it has been replaced).
	if err = f.WaitUntilExpectedRoutesInConfig(context.WithTimeout(waitTimeout), natsCluster); err != nil {
		t.Fatal(err)
	}
}
