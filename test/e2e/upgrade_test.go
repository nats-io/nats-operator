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
	"testing"

	"github.com/pires/nats-operator/pkg/util/k8sutil"
	"github.com/pires/nats-operator/test/e2e/framework"

	"k8s.io/kubernetes/pkg/api"
)

func TestNATSUpgrade(t *testing.T) {
	f := framework.Global

	originalVersion := "0.9.2"
	newVersion := "0.9.4"

	original := makeClusterSpec("test-nats-", 3)
	original = clusterWithVersion(original, originalVersion)
	test, err := createCluster(f, original)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := deleteCluster(f, test.Name); err != nil {
			t.Fatal(err)
		}
	}()

	_, err = waitSizeReachedWithFilter(f, test.Name, 3, 60, func(pod *api.Pod) bool {
		return k8sutil.GetNATSVersion(pod) == originalVersion
	})
	if err != nil {
		t.Fatalf("failed to create 3 peers cluster: %v", err)
	}

	test = clusterWithVersion(test, newVersion)

	if _, err := updateCluster(f, test); err != nil {
		t.Fatalf("fail to update cluster version: %v", err)
	}

	_, err = waitSizeReachedWithFilter(f, test.Name, 3, 60, func(pod *api.Pod) bool {
		return k8sutil.GetNATSVersion(pod) == newVersion
	})
	if err != nil {
		t.Fatalf("failed to wait for new version of NATS cluster: %v", err)
	}
}
