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
	"testing"
	"time"

	"github.com/pires/nats-operator/pkg/spec"
	"github.com/pires/nats-operator/test/e2e/e2eutil"
	"github.com/pires/nats-operator/test/e2e/framework"
)

func TestCreateCluster(t *testing.T) {
	f := framework.Global
	testNats, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2eutil.NewCluster("test-nats-", 3))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testNats); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 6, testNats); err != nil {
		t.Fatalf("failed to create 3 members NATS cluster: %v", err)
	}
}

// TestPauseControl tests the user can pause the operator from controlling
// a NATS cluster.
func TestPauseControl(t *testing.T) {
	f := framework.Global
	testNats, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2eutil.NewCluster("test-nats-", 3))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testNats); err != nil {
			t.Fatal(err)
		}
	}()

	names, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 6, testNats)
	if err != nil {
		t.Fatalf("failed to create 3 members NATS cluster: %v", err)
	}

	updateFunc := func(cl *spec.NatsCluster) {
		cl.Spec.Paused = true
	}
	if testNats, err = e2eutil.UpdateCluster(f.CRClient, testNats, 10, updateFunc); err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// TODO: this is used to wait for the CR to be updated.
	// TODO: make this wait for reliable
	time.Sleep(5 * time.Second)

	if err := e2eutil.KillMembers(f.KubeClient, f.Namespace, names[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 2, 1, testNats); err != nil {
		t.Fatalf("failed to wait for killed member to die: %v", err)
	}
	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 1, testNats); err == nil {
		t.Fatalf("cluster should not be recovered: control is paused")
	}

	updateFunc = func(cl *spec.NatsCluster) {
		cl.Spec.Paused = false
	}
	if testNats, err = e2eutil.UpdateCluster(f.CRClient, testNats, 10, updateFunc); err != nil {
		t.Fatalf("failed to resume control: %v", err)
	}

	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 6, testNats); err != nil {
		t.Fatalf("failed to resize to 3 members NATS cluster: %v", err)
	}
}

func TestNatsUpgrade(t *testing.T) {
	f := framework.Global
	origNats := e2eutil.NewCluster("test-nats-", 3)
	origNats = e2eutil.ClusterWithVersion(origNats, "1.0.0")
	testNats, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, origNats)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testNats); err != nil {
			t.Fatal(err)
		}
	}()

	err = e2eutil.WaitSizeAndVersionReached(t, f.KubeClient, "1.0.0", 3, 6, testNats)
	if err != nil {
		t.Fatalf("failed to create 3 members NATS cluster: %v", err)
	}

	updateFunc := func(cl *spec.NatsCluster) {
		cl = e2eutil.ClusterWithVersion(cl, "1.0.2")
	}
	_, err = e2eutil.UpdateCluster(f.CRClient, testNats, 10, updateFunc)
	if err != nil {
		t.Fatalf("fail to update cluster version: %v", err)
	}

	// We have seen in k8s 1.7.1 env it took 35s for the pod to restart with the new image.
	err = e2eutil.WaitSizeAndVersionReached(t, f.KubeClient, "1.0.2", 3, 10, testNats)
	if err != nil {
		t.Fatalf("failed to wait new version NATS cluster: %v", err)
	}
}

func TestResizeCluster3To5(t *testing.T) {
	f := framework.Global
	testNats, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2eutil.NewCluster("test-nats-", 3))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testNats); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 6, testNats); err != nil {
		t.Fatalf("failed to create NATS cluster with 3 members: %v", err)
	}

	updateFunc := func(cl *spec.NatsCluster) {
		cl = e2eutil.ClusterWithSize(cl, 5)
	}
	_, err = e2eutil.UpdateCluster(f.CRClient, testNats, 10, updateFunc)
	if err != nil {
		t.Fatalf("fail to update cluster version: %v", err)
	}

	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, 6, testNats); err != nil {
		t.Fatalf("failed to resize NATS cluster to 5 members: %v", err)
	}
}

func TestResizeCluster5To3(t *testing.T) {
	f := framework.Global
	testNats, err := e2eutil.CreateCluster(t, f.CRClient, f.Namespace, e2eutil.NewCluster("test-nats-", 5))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := e2eutil.DeleteCluster(t, f.CRClient, f.KubeClient, testNats); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 5, 6, testNats); err != nil {
		t.Fatalf("failed to create NATS cluster with 5 members: %v", err)
	}

	updateFunc := func(cl *spec.NatsCluster) {
		cl = e2eutil.ClusterWithSize(cl, 3)
	}
	_, err = e2eutil.UpdateCluster(f.CRClient, testNats, 10, updateFunc)
	if err != nil {
		t.Fatalf("fail to update cluster version: %v", err)
	}

	if _, err := e2eutil.WaitUntilPodSizeReached(t, f.KubeClient, 3, 6, testNats); err != nil {
		t.Fatalf("failed to resize NATS cluster to 3 members: %v", err)
	}
}
