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
	"github.com/nats-io/nats-operator/pkg/conf"
	"github.com/nats-io/nats-operator/pkg/constants"
	"k8s.io/api/core/v1"
	watchapi "k8s.io/apimachinery/pkg/watch"
)

// TestCreateClusterWithTLSConfig creates a NatsCluster resource with TLS enabled and waits for the full mesh to be formed.
func TestCreateClusterWithTLSConfig(t *testing.T) {
	var (
		size    = 3
		version = "1.3.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with three members, and with TLS enabled
	natsCluster, err = f.CreateCluster(f.Namespace, "", size, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		// The NatsCluster resource must be called "nats" in order for the pre-provisioned certificates to work.
		natsCluster.Name = "nats"
		// Enable TLS using pre-provisioned certificates.
		natsCluster.Spec.TLS = &natsv1alpha2.TLSConfig{
			ServerSecret: "nats-certs",
			RoutesSecret: "nats-routes-tls",
		}
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

	// Wait until the full mesh is formed.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMeshWithVersion(ctx1, natsCluster, size, version); err != nil {
		t.Fatal(err)
	}

	// Wait for the "TLS required for client connections" log message to appear in the logs for the very first pod.
	ctx2, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilPodLogLineMatches(ctx2, natsCluster, 1, "TLS required for client connections"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateClusterWithHTTPSConfig(t *testing.T) {
	var (
		size    = 1
		version = "1.4.0"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with three members, and with TLS enabled
	natsCluster, err = f.CreateCluster(f.Namespace, "", size, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		// The NatsCluster resource must be called "nats" in
		// order for the pre-provisioned certificates to work.
		natsCluster.Name = "nats"

		// Enable TLS using pre-provisioned certificates.
		natsCluster.Spec.TLS = &natsv1alpha2.TLSConfig{
			EnableHttps:  true,
			ServerSecret: "nats-certs",
			RoutesSecret: "nats-routes-tls",
		}
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

	// Wait until the full mesh is formed.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	err = f.WaitUntilSecretCondition(ctx1, natsCluster, func(event watchapi.Event) (bool, error) {
		secret := event.Object.(*v1.Secret)
		conf, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}
		config, err := natsconf.Unmarshal(conf)
		if err != nil {
			return false, nil
		}
		if config.HTTPSPort != 8222 || config.HTTPPort != 0 {
			return false, nil
		}

		pods, err := f.PodsForNatsCluster(natsCluster)
		if err != nil {
			return false, nil
		}
		if len(pods) < 1 {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateClusterWithVerifyAndMap(t *testing.T) {
	natsCluster, err := f.CreateCluster(f.Namespace, "", 1, "", func(natsCluster *natsv1alpha2.NatsCluster) {
		// The NatsCluster resource must be called "nats" in
		// order for the pre-provisioned certificates to work.
		natsCluster.Name = "nats-verify"
		natsCluster.Spec.ServerImage = "wallyqs/nats-server"
		natsCluster.Spec.Version = "edge-2.0.0-RC5"

		// Enable TLS using pre-provisioned certificates.
		natsCluster.Spec.TLS = &natsv1alpha2.TLSConfig{
			ServerSecret: "nats-certs",
			RoutesSecret: "nats-routes-tls",
		}
		natsCluster.Spec.Auth = &natsv1alpha2.AuthConfig{
			TLSVerifyAndMap: true,
		}
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

	// Wait until the full mesh is formed.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	err = f.WaitUntilSecretCondition(ctx1, natsCluster, func(event watchapi.Event) (bool, error) {
		secret := event.Object.(*v1.Secret)
		conf, ok := secret.Data[constants.ConfigFileName]
		if !ok {
			return false, nil
		}
		config, err := natsconf.Unmarshal(conf)
		if err != nil {
			return false, nil
		}
		if config.TLS == nil || !config.TLS.VerifyAndMap {
			return false, nil
		}

		pods, err := f.PodsForNatsCluster(natsCluster)
		if err != nil {
			return false, nil
		}
		if len(pods) < 1 {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
