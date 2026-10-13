/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package gpumode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyFallbackPreservesModeOmission(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"portion", `{"gpuMemoryPortionRequest":0.25}`, true},
		{"legacy whole GPU", `{"gpuPortionRequest":2}`, true},
		{"devices", `{"gpuDevicesRequest":2}`, true},
		{"absolute memory", `{"gpuMemoryRequest":"1Gi"}`, true},
		{"CPU only", `{"gpuDevicesRequest":0}`, false},
		{"no allocation", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, replica := range []string{"worker", "master", "leader"} {
				t.Run(replica, func(t *testing.T) {
					var compute map[string]interface{}
					require.NoError(t, json.Unmarshal([]byte(tc.body), &compute))
					values := map[string]interface{}{replica: map[string]interface{}{"compute": compute}}
					apply, cleanup := PolicyFallback()
					apply(values)
					if tc.want {
						require.Equal(t, TimeSlicing, compute["gpuComputeMode"])
					} else {
						require.NotContains(t, compute, "gpuComputeMode")
					}
					cleanup()
					require.NotContains(t, compute, "gpuComputeMode")
					require.NotContains(t, compute, "computeMode")
				})
			}
		})
	}
}

func TestPolicyFallbackPreservesConfiguredMode(t *testing.T) {
	compute := map[string]interface{}{"gpuDevicesRequest": 1, "gpuComputeMode": SMSharing}
	arbitrary := map[string]interface{}{"compute": map[string]interface{}{"gpuDevicesRequest": 1}}
	values := map[string]interface{}{"compute": compute, "annotations": arbitrary, "defaults": arbitrary, "rules": arbitrary}
	apply, cleanup := PolicyFallback()
	apply(values)
	cleanup()
	require.Equal(t, SMSharing, compute["gpuComputeMode"])
	require.NotContains(t, arbitrary["compute"], "gpuComputeMode")
}

func TestUpdateProjectsOnlyMode(t *testing.T) {
	resolved := map[string]interface{}{"compute": map[string]interface{}{"gpuDevicesRequest": 1, "gpuComputeMode": SMSharing}}
	submitted := map[string]interface{}{"compute": map[string]interface{}{"cpuCoreRequest": 1}}
	ProjectResolvedModes(resolved, submitted)
	compute := submitted["compute"].(map[string]interface{})
	require.Equal(t, SMSharing, compute["gpuComputeMode"])
	require.NotContains(t, compute, "gpuDevicesRequest")
	require.NotContains(t, compute, "computeMode")
}

func TestUnchangedComputePreservesImplicitMode(t *testing.T) {
	compute := map[string]interface{}{"gpuDevicesRequest": 1}
	values := map[string]interface{}{"compute": compute}
	cleanup := PreserveImplicitModeForInferenceUpdate(values)
	ApplyModeDefaults(values, map[string]interface{}{"compute": map[string]interface{}{"gpuComputeMode": SMSharing}})
	require.Equal(t, TimeSlicing, compute["gpuComputeMode"])
	cleanup()
	require.NotContains(t, compute, "gpuComputeMode")
	require.NotContains(t, compute, "computeMode")
}
