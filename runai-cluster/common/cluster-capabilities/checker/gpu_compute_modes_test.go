/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package checker

import (
	"context"
	"encoding/json"

	openapi "github.com/dsx-ai-factory/run-ai/control-plane/cluster-service/pkg/client/generated"
	"github.com/dsx-ai-factory/run-ai/runai-cluster/common/cluster-capabilities/clients/cluster"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/cache/v2"
	cache_testing "github.com/dsx-ai-factory/run-ai/runai-common-packages/cache/v2/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GPU compute mode configuration", func() {
	DescribeTable("reads only explicit mode availability",
		func(statusJSON string, timeSlicing, smSharing bool) {
			displayed := &openapi.DisplayedCluster{}
			Expect(json.Unmarshal([]byte(statusJSON), displayed)).To(Succeed())
			modes := GpuComputeModes(displayed)
			Expect(modes).To(HaveLen(2))
			Expect(modes["time-slicing"].Available).To(Equal(timeSlicing))
			Expect(modes["sm-sharing"].Available).To(Equal(smSharing))
		},
		Entry("no status even on a newer version", `{"version":"99.0.0"}`, false, false),
		Entry("null status", `{"status":null}`, false, false),
		Entry("no dependencies", `{"status":{}}`, false, false),
		Entry("legacy GPU-stack readiness", `{"status":{"dependencies":{"required":{"gpu-stack":{"available":true}}}}}`, false, false),
		Entry("parent without per-mode evidence", `{"status":{"dependencies":{"optional":{"gpu-compute-modes":{"available":true}}}}}`, false, false),
		Entry("both modes", `{"status":{"dependencies":{"optional":{"gpu-compute-modes":{"available":true,"components":{"time-slicing":{"available":true},"sm-sharing":{"available":true}}}}}}}`, true, true),
		Entry("SM disabled", `{"status":{"dependencies":{"optional":{"gpu-compute-modes":{"available":true,"components":{"time-slicing":{"available":true},"sm-sharing":{"available":false}}}}}}}`, true, false),
		Entry("missing SM report", `{"status":{"dependencies":{"optional":{"gpu-compute-modes":{"available":true,"components":{"time-slicing":{"available":true}}}}}}}`, true, false),
		Entry("parent unavailable overrides stale children",
			`{"status":{"dependencies":{"optional":{"gpu-compute-modes":{"available":false,"components":{"time-slicing":{"available":true},"sm-sharing":{"available":true}}}}}}}`,
			false, false),
		Entry("unknown future mode", `{"status":{"dependencies":{"optional":{"gpu-compute-modes":{"available":true,"components":{"future-mode":{"available":true}}}}}}}`, false, false),
	)

	It("propagates availability and its reason through the cluster checker cache", func() {
		cache_testing.EnableCache()
		InitClustersCache(defaultCheckerClusterConfig)
		Expect(cache.ClearStore[clusterInfo]()).To(Succeed())
		DeferCleanup(func() { Expect(cache.ClearStore[clusterInfo]()).To(Succeed()) })
		displayed := &openapi.DisplayedCluster{}
		Expect(json.Unmarshal([]byte(`{
			"uuid":"mode-cluster","version":"3.0.0",
			"status":{"dependencies":{"optional":{"gpu-compute-modes":{
				"available":true,"components":{
					"time-slicing":{"available":true},
					"sm-sharing":{"available":false,"reason":"SM-sharing disabled"}
				}
			}}}}
		}`), displayed)).To(Succeed())
		mock := cluster.NewSimpleMockClient().WithCluster("mode-cluster", *displayed)
		cluster.SetMock(mock)
		for range 2 {
			checker := NewClusterCapabilitiesChecker("mode-cluster")
			configuration, err := checker.ClusterConfiguration(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(configuration.GpuComputeModes["time-slicing"].Available).To(BeTrue())
			Expect(configuration.GpuComputeModes["sm-sharing"]).To(Equal(GpuComputeModeStatus{Reason: "SM-sharing disabled"}))
		}
		Expect(*mock.GetClusterByIdCallCount).To(Equal(1))
	})

	It("preserves the parent failure reason for both modes", func() {
		displayed := &openapi.DisplayedCluster{}
		Expect(json.Unmarshal([]byte(`{"status":{"dependencies":{"optional":{"gpu-compute-modes":{"available":false,"reason":"runtime unavailable"}}}}}`), displayed)).To(Succeed())
		for _, mode := range GpuComputeModes(displayed) {
			Expect(mode).To(Equal(GpuComputeModeStatus{Reason: "runtime unavailable"}))
		}
	})
})
