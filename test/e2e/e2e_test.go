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

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/pires/nats-operator/test/e2e/framework"
)

func TestCreateCluster(t *testing.T) {
	f := framework.Global
	test, err := createCluster(f, makeClusterSpec("test-nats-", 3))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := deleteCluster(f, test.Name); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := waitUntilSizeReached(f, test.Name, 3, 60); err != nil {
		t.Fatalf("failed to create 3 peers cluster: %v", err)
	}
}

func TestResizeCluster3to5(t *testing.T) {
	f := framework.Global
	test, err := createCluster(f, makeClusterSpec("test-nats-", 3))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := deleteCluster(f, test.Name); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := waitUntilSizeReached(f, test.Name, 3, 60); err != nil {
		t.Fatalf("failed to create 3 peers cluster: %v", err)
		return
	}
	fmt.Println("reached 3 peers cluster")

	test.Spec.Size = 5
	if _, err := updateCluster(f, test); err != nil {
		t.Fatal(err)
	}

	if _, err := waitUntilSizeReached(f, test.Name, 5, 60); err != nil {
		t.Fatalf("failed to resize to 5 peers: %v", err)
	}
}

func TestResizeCluster5to3(t *testing.T) {
	f := framework.Global
	test, err := createCluster(f, makeClusterSpec("test-nats-", 5))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := deleteCluster(f, test.Name); err != nil {
			t.Fatal(err)
		}
	}()

	if _, err := waitUntilSizeReached(f, test.Name, 5, 90); err != nil {
		t.Fatalf("failed to create 5 peers cluster: %v", err)
		return
	}
	fmt.Println("reached 5 peers cluster")

	test.Spec.Size = 3
	if _, err := updateCluster(f, test); err != nil {
		t.Fatal(err)
	}

	if _, err := waitUntilSizeReached(f, test.Name, 3, 60); err != nil {
		t.Fatalf("failed to resize to 3 peers: %v", err)
	}
}

func TestOneMemberRecovery(t *testing.T) {
	f := framework.Global
	test, err := createCluster(f, makeClusterSpec("test-nats-", 3))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := deleteCluster(f, test.Name); err != nil {
			t.Fatal(err)
		}
	}()

	names, err := waitUntilSizeReached(f, test.Name, 3, 60)
	if err != nil {
		t.Fatalf("failed to create 3 peers cluster: %v", err)
		return
	}
	fmt.Println("reached 3 peers cluster")

	if err := killMembers(f, names[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := waitUntilSizeReached(f, test.Name, 3, 60); err != nil {
		t.Fatalf("failed to recover missing peer: %v", err)
	}
}

// TestPauseControl tests the user can pause the operator from controlling
// a NATS cluster.
func TestPauseControl(t *testing.T) {
	f := framework.Global
	test, err := createCluster(f, makeClusterSpec("test-nats-", 3))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := deleteCluster(f, test.Name); err != nil {
			t.Fatal(err)
		}
	}()

	names, err := waitUntilSizeReached(f, test.Name, 3, 60)
	if err != nil {
		t.Fatalf("failed to create 3 peers cluster: %v", err)
	}

	test.Spec.Paused = true
	if test, err = updateCluster(f, test); err != nil {
		t.Fatalf("failed to pause control: %v", err)
	}

	// TODO: this is used to wait for the TPR to be updated.
	// TODO: make this wait for reliable
	time.Sleep(5 * time.Second)

	if err := killMembers(f, names[0]); err != nil {
		t.Fatal(err)
	}

	if _, err := waitUntilSizeReached(f, test.Name, 2, 30); err != nil {
		t.Fatalf("failed to wait for killed peer to die: %v", err)
	}
	if _, err := waitUntilSizeReached(f, test.Name, 3, 30); err == nil {
		t.Fatalf("cluster should not be recovered: control is paused")
	}

	test.Spec.Paused = false
	if _, err = updateCluster(f, test); err != nil {
		t.Fatalf("failed to resume control: %v", err)
	}

	if _, err := waitUntilSizeReached(f, test.Name, 3, 60); err != nil {
		t.Fatalf("failed to resize to 3 peers cluster: %v", err)
	}
}
