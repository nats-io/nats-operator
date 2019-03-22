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
	"time"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/conf"
	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"
	"k8s.io/api/core/v1"
	watchapi "k8s.io/apimachinery/pkg/watch"
)

// TestCreateCluster creates a NatsCluster resource and waits for the full mesh to be formed.
func TestCreateCluster(t *testing.T) {
	var (
		size    = 3
		version = "1.3.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with three members.
	if natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-", size, version); err != nil {
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

// TestPauseControl creates a NatsCluster resource and waits for the full mesh to be formed.
// Then, it pauses control of the NatsCluster resource and scales it up to five nodes, expecting the operation to NOT be performed.
// Finally, it resumes control of the NatsCluster resource and waits for the full five-node mesh to be formed.
func TestPauseControl(t *testing.T) {
	var (
		initialSize = 3
		finalSize   = 5
		version     = "1.3.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with three members.
	if natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-", initialSize, version); err != nil {
		t.Fatal(err)
	}
	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(natsCluster); err != nil {
			t.Error(err)
		}
	}()

	// Wait until the full mesh is formed.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx1, natsCluster, initialSize, version); err != nil {
		t.Fatal(err)
	}

	// Pause control of the cluster.
	natsCluster.Spec.Paused = true
	if natsCluster, err = f.PatchCluster(natsCluster); err != nil {
		t.Fatal(err)
	}

	// Scale the cluster up to five members
	natsCluster.Spec.Size = finalSize
	if natsCluster, err = f.PatchCluster(natsCluster); err != nil {
		t.Fatal(err)
	}
	// Make sure that the full mesh is NOT formed with the current size (5) within the timeout period.
	ctx2, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx2, natsCluster, finalSize, version); err == nil {
		t.Fatalf("the full mesh has formed while control is paused")
	}

	// Resume control of the cluster.
	natsCluster.Spec.Paused = false
	if natsCluster, err = f.PatchCluster(natsCluster); err != nil {
		t.Fatal(err)
	}
	// Make sure that the full mesh is formed with the current size, since control has been resumed.
	ctx3, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx3, natsCluster, finalSize, version); err != nil {
		t.Fatal(err)
	}
}

// TestCreateClusterWithHostPort creates a NatsCluster resource using
// a host port with no advertise for clients.
func TestCreateClusterWithHostPort(t *testing.T) {
	var (
		size    = 1
		version = "1.3.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with three members.
	natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-", size, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		natsCluster.Spec.Pod = &natsv1alpha2.PodPolicy{
			EnableClientsHostPort: true,
		}
		natsCluster.Spec.NoAdvertise = true
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

	// List pods belonging to the NATS cluster, and make
	// sure that every route is listed in the
	// configuration.
	var attempts int
	for range time.NewTicker(1 * time.Second).C {
		if attempts >= 30 {
			t.Fatalf("Timed out waiting for pods with host port")
		}
		attempts++

		pods, err := f.PodsForNatsCluster(natsCluster)
		if err != nil {
			continue
		}
		if len(pods) == 0 {
			continue
		}

		var foundNoAdvertise bool
		pod := pods[0]
		container := pod.Spec.Containers[0]
		for _, v := range container.Command {
			if v == "--no_advertise" {
				foundNoAdvertise = true
			}
		}
		if !foundNoAdvertise {
			t.Error("Container not configured with no advertise")
		}

		var foundHostPort bool
		for _, port := range container.Ports {
			if port.ContainerPort == int32(4222) && port.HostPort == int32(4222) {
				foundHostPort = true
			}
		}
		if !foundHostPort {
			t.Error("Container not configured with host port")
		}

		break
	}
}

func TestCreateClustersWithExtraRoutes(t *testing.T) {
	var (
		size    = 3
		version = "1.4.0"
	)

	var (
		ncA *natsv1alpha2.NatsCluster
		ncB *natsv1alpha2.NatsCluster
		err error
	)

	ncA, err = f.CreateCluster(f.Namespace, "test-nats-", size, version,
		func(natsCluster *natsv1alpha2.NatsCluster) {})
	if err != nil {
		t.Fatal(err)
	}

	// Mesh the cluster with the other cluster.
	ncB, err = f.CreateCluster(f.Namespace, "test-nats-", 1, version,
		func(natsCluster *natsv1alpha2.NatsCluster) {
			natsCluster.Spec.ExtraRoutes = []*natsv1alpha2.ExtraRoute{
				{Cluster: ncA.Name},
				{Route: "nats://127.0.0.1:6222"},
			}

			// Use a host port to confirm the routes
			natsCluster.Spec.Pod = &natsv1alpha2.PodPolicy{
				EnableClientsHostPort: true,
			}
		})
	if err != nil {
		t.Fatal(err)
	}

	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(ncA); err != nil {
			t.Error(err)
		}
		if err = f.DeleteCluster(ncB); err != nil {
			t.Error(err)
		}
	}()

	ctx, done := context.WithTimeout(context.Background(), 120*time.Second)
	defer done()
	err = f.WaitUntilSecretCondition(ctx, ncB, func(event watchapi.Event) (bool, error) {
		// Grab the secret from the event.
		secret := event.Object.(*v1.Secret)
		// Make sure that the "nats.conf" key is present in the secret.
		conf, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}

		// Grab the ServerConfig object that corresponds to "nats.conf".
		config, err := natsconf.Unmarshal(conf)
		if err != nil {
			return false, nil
		}
		if config.Cluster == nil || config.Cluster.Routes == nil {
			return false, nil
		}
		routes := config.Cluster.Routes
		if len(routes) == 3 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateServerWithCustomConfig(t *testing.T) {
	var (
		size    = 1
		version = "1.4.0"
		nc      *natsv1alpha2.NatsCluster
		err     error
	)
	nc, err = f.CreateCluster(f.Namespace, "test-nats-", size, version,
		func(natsCluster *natsv1alpha2.NatsCluster) {
			// Custom config
			natsCluster.Spec.ServerConfig = &natsv1alpha2.ServerConfig{
				Debug:            true,
				Trace:            true,
				MaxConnections:   10,
				MaxControlLine:   2048,
				MaxPayload:       128,
				MaxPending:       65536,
				MaxSubscriptions: 200,
				WriteDeadline:    "10s",
				DisableLogtime:   true,
			}
		})
	if err != nil {
		t.Fatal(err)
	}

	// Make sure we cleanup the NatsCluster resource after we're done testing.
	defer func() {
		if err = f.DeleteCluster(nc); err != nil {
			t.Error(err)
		}
	}()

	ctx, done := context.WithTimeout(context.Background(), 120*time.Second)
	defer done()
	err = f.WaitUntilSecretCondition(ctx, nc, func(event watchapi.Event) (bool, error) {
		secret := event.Object.(*v1.Secret)
		conf, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}
		config, err := natsconf.Unmarshal(conf)
		if err != nil {
			return false, nil
		}
		if !config.Debug || !config.Trace {
			return false, nil
		}
		if config.WriteDeadline != "10s" {
			return false, nil
		}
		if config.MaxPayload != 128 {
			return false, nil
		}
		if config.MaxPending != 65536 {
			return false, nil
		}
		if config.MaxConnections != 10 {
			return false, nil
		}
		if config.Logtime {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndDeleteClusterDependencies(t *testing.T) {
	var (
		size        = 1
		version     = "1.4.0"
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)
	if natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-", size, version); err != nil {
		t.Fatal(err)
	}

	ctx, done := context.WithTimeout(context.Background(), waitTimeout)
	defer done()

	err = f.WaitUntilSecretCondition(ctx, natsCluster, func(event watchapi.Event) (bool, error) {
		// Just check that the secret is present.
		secret := event.Object.(*v1.Secret)
		_, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}

		// Wait for a single pod to be created.
		pods, err := f.PodsForNatsCluster(natsCluster)
		if err != nil {
			return false, err
		}
		if len(pods) < 1 {
			return false, nil
		}

		// Confirm that there is a service for the NatsCluster.
		svcs, err := f.ServicesForNatsCluster(natsCluster)
		if err != nil {
			return false, err
		}
		if len(svcs) < 1 {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Delete the cluster and there should be no more pods,
	// secrets or services.
	if err = f.DeleteCluster(natsCluster); err != nil {
		t.Error(err)
	}

	// Wait for all the pods to report the expected routes and version.
	err = retryutil.RetryWithContext(ctx, 5*time.Second, func() (bool, error) {
		// Wait for the single pod to be deleted.
		pods, err := f.PodsForNatsCluster(natsCluster)
		if err != nil {
			return false, err
		}
		if len(pods) > 0 {
			return false, nil
		}

		// Confirm that there is a service for the NatsCluster
		// and it is gone now.
		svcs, err := f.ServicesForNatsCluster(natsCluster)
		if err != nil {
			return false, err
		}
		if len(svcs) > 0 {
			return false, nil
		}

		secrets, err := f.SecretsForNatsCluster(natsCluster)
		if err != nil {
			return false, err
		}
		if len(secrets) > 0 {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
