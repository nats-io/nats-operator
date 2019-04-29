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
	"sort"
	"sync"
	"testing"
	"time"

	natsv1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
)

// podLDMResult captures information about the time at which a pod was placed in "lame duck" mode.
type podLDMResult struct {
	err       error
	podIndex  int
	timestamp time.Time
}

// TestLameDuckModeWhenScalingDown creates a NatsCluster resource with
// five members and waits for the full mesh to be formed.  Then, it
// sets a size of 3 in the NatsCluster resource and waits for the
// scale-down operation to complete while making sure that each pod has
// been placed in the "lame duck" mode.
func TestLameDuckModeWhenScalingDown(t *testing.T) {
	var (
		initialSize = 3
		finalSize   = 1
		// TODO Replace with an adequate stable tag once there is one.
		version = "5d86964"
		// TODO Remove once the "nats" image has an adequate stable tag.
		serverImage = "natsop2018/gnatsd"
	)

	var (
		natsCluster *natsv1alpha2.NatsCluster
		err         error
	)

	// Create a NatsCluster resource with the initial size.
	natsCluster, err = f.CreateCluster(f.Namespace, "test-nats-", initialSize, version, func(natsCluster *natsv1alpha2.NatsCluster) {
		natsCluster.Spec.ServerImage = serverImage
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

	// Wait until the full mesh is formed with the initial size.
	// TODO: Replace with WaitUntilFullMeshWithVersion when a stable version of NATS with LDM support is released.
	ctx1, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMesh(ctx1, natsCluster, initialSize); err != nil {
		t.Fatal(err)
	}

	// For every pod being removed from the cluster, we wait for a log message indicating the "gnatsd" process has entered the "lame duck" mode.
	// We capture the pod's index, and either the timestamp at which we've seen the log message or an error.
	// This is done so we can later assert that pods have entered the "lame duck" mode in the expected order.
	var wg sync.WaitGroup
	wg.Add(initialSize - finalSize)
	resCh := make(chan podLDMResult, initialSize-finalSize)
	for idx := initialSize; idx > finalSize; idx-- {
		go func(idx int) {
			defer wg.Done()
			ctx, fn := context.WithTimeout(context.Background(), waitTimeout)
			defer fn()
			if err := f.WaitUntilPodLogLineMatches(ctx, natsCluster, idx, "Entering lame duck mode, stop accepting new clients"); err != nil {
				resCh <- podLDMResult{err: err, podIndex: idx}
			} else {
				resCh <- podLDMResult{podIndex: idx, timestamp: time.Now()}
			}
		}(idx)
	}

	// Scale the cluster down.
	natsCluster.Spec.Size = finalSize
	if natsCluster, err = f.PatchCluster(natsCluster); err != nil {
		t.Fatal(err)
	}

	// Wait until the full mesh is formed with the final size.
	// TODO: Replace with WaitUntilFullMeshWithVersion when a stable version of NATS with LDM support is released.
	ctx2, fn := context.WithTimeout(context.Background(), waitTimeout)
	defer fn()
	if err = f.WaitUntilFullMesh(ctx2, natsCluster, finalSize); err != nil {
		t.Fatal(err)
	}

	// Wait for all the goroutines to terminate and close the results channel.
	wg.Wait()
	close(resCh)

	// Iterate over the results sent over "resCh" and make sure that pods have entered LDM by (reverse) order of their indexes.
	results := make([]podLDMResult, 0, len(resCh))
	for res := range resCh {
		results = append(results, res)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].timestamp.Before(results[j].timestamp)
	})
	for idx := 1; idx < len(results); idx++ {
		curr := results[idx]
		prev := results[idx-1]
		if curr.podIndex >= prev.podIndex {
			t.Fatalf("pods entered the \"lame duck\" mode in unexpected order: %+v", results)
		}
	}
}
