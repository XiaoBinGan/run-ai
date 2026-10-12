/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package checker

import clusterService "github.com/dsx-ai-factory/run-ai/control-plane/cluster-service/pkg/client/generated"

// GpuComputeModes reads runtime-reported availability independently of cluster version.
func GpuComputeModes(cluster *clusterService.DisplayedCluster) map[string]GpuComputeModeStatus {
	modes := map[string]GpuComputeModeStatus{
		"time-slicing": {Reason: "GPU compute mode availability has not been reported"},
		"sm-sharing":   {Reason: "GPU compute mode availability has not been reported"},
	}
	if cluster == nil {
		return modes
	}
	status := cluster.Status.Get()
	if status == nil {
		return modes
	}
	dependency, found := status.GetDependencies().Optional[GpuComputeModesDependency]
	if !found {
		return modes
	}
	for mode, unavailable := range modes {
		if !dependency.Available {
			if reason := dependency.GetReason(); reason != "" {
				unavailable.Reason = reason
			}
			modes[mode] = unavailable
			continue
		}
		if dependency.Components == nil {
			continue
		}
		if component, found := (*dependency.Components)[mode]; found {
			modes[mode] = GpuComputeModeStatus{Available: component.Available, Reason: component.GetReason()}
		}
	}
	return modes
}
