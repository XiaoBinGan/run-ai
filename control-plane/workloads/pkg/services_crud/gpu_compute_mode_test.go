/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services_crud

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/gpumode"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/internal/generated/pkg/api"
	workloadsApi "github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/client/generated"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGpuModePolicyPipeline(t *testing.T) {
	for _, tc := range []struct {
		name, request, def, source, want string
		rules                            map[string]interface{}
		fail                             bool
	}{
		{name: "fallback"},
		{name: "administrator default", def: "sm-sharing", want: "sm-sharing"},
		{name: "editable explicit wins", request: "time-slicing", def: "sm-sharing", want: "time-slicing"},
		{name: "locked equal", request: "sm-sharing", def: "sm-sharing", rules: map[string]interface{}{"canEdit": false}, want: "sm-sharing"},
		{name: "locked conflict", request: "time-slicing", def: "sm-sharing", rules: map[string]interface{}{"canEdit": false}, fail: true},
		{name: "derived before builtin", source: "sm-sharing", rules: map[string]interface{}{"defaultFrom": map[string]interface{}{"field": "image"}}, want: "sm-sharing"},
		{name: "fallback checked against options", rules: map[string]interface{}{"options": []interface{}{map[string]interface{}{"value": "sm-sharing"}}}, fail: true},
		{name: "fallback checked against blocked", rules: map[string]interface{}{"blocked": true}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compute := map[string]interface{}{"gpuMemoryPortionRequest": json.Number("0.25")}
			if tc.request != "" {
				compute["gpuComputeMode"] = tc.request
			}
			input := map[string]interface{}{"compute": compute}
			if tc.source != "" {
				input["image"] = tc.source
			}
			defaults := map[string]interface{}{}
			if tc.def != "" {
				defaults["compute"] = map[string]interface{}{"gpuComputeMode": tc.def}
			}
			rules := map[string]interface{}{"compute": map[string]interface{}{"gpuComputeMode": tc.rules}}
			mutated, err := MutateWorkload(input, defaults, false, nil, nil)
			require.Nil(t, err)
			require.NotContains(t, mutated["compute"], "computeMode")
			validationErr := validateSpecAgainstPolicy(Workspace, rules, defaults, mutated)
			if tc.fail {
				require.NotNil(t, validationErr)
				require.Equal(t, int32(400), validationErr.Code)
				return
			}
			require.Nil(t, validationErr)
			if tc.request == "" && tc.def == "" && tc.source == "" {
				require.NotContains(t, mutated["compute"], "gpuComputeMode")
			}
			spec, _, buildErr := buildResponseSpecFromMap[workloadsApi.SupersetSpec, workloadsApi.MasterSpec](Workspace, mutated)
			require.Nil(t, buildErr)
			params := createWorkloadParams{Response: workloadsApi.SupersetWorkload{Spec: spec}}
			stored := NewWorkloadFromWorkloadParams(&params, nil)
			response := WorkloadFromDbRecord(stored, workloadsApi.PHASE_CREATING)
			require.Equal(t, tc.want, string(response.Spec.Compute.Get().GetGpuComputeMode()))
			snapshot := NewWorkloadV2SnapshotFromWorkloadParams(&params, nil, "")
			require.Equal(t, tc.want, string(snapshot.Spec.Compute.Get().GetGpuComputeMode()))
			data, marshalErr := json.Marshal(response)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(data), `"computeMode"`)
		})
	}
}

func TestGpuModeUpdateAndLegacyGuard(t *testing.T) {
	var full workloadsApi.SupersetSpec
	require.NoError(t, json.Unmarshal([]byte(`{"compute":{"gpuDevicesRequest":1,"gpuComputeMode":"sm-sharing"}}`), &full))
	require.NotNil(t, validateNativeGpuMode(&full))
	var update api.InferenceUpdateSpecSpec
	require.NoError(t, json.Unmarshal([]byte(`{"compute":{"gpuDevicesRequest":1}}`), &update))
	require.Nil(t, mergeUpdateSpecIntoFullSpec(context.Background(), &full, &update))
	require.False(t, full.Compute.Get().GpuComputeMode.IsSet())
	require.Nil(t, validateNativeGpuMode(&full))
	require.Nil(t, validateNativeGpuMode((*workloadsApi.MasterSpec)(nil)))
}

func TestGpuModesForReplicas(t *testing.T) {
	input := map[string]interface{}{
		"worker": map[string]interface{}{"compute": map[string]interface{}{"gpuMemoryPortionRequest": 0.25, "gpuComputeMode": "sm-sharing"}},
		"master": map[string]interface{}{"compute": map[string]interface{}{"gpuDevicesRequest": 1, "gpuComputeMode": "time-slicing"}},
	}
	for _, same := range []bool{false, true} {
		t.Run(fmt.Sprintf("masterSameAsWorker=%v", same), func(t *testing.T) {
			request := input
			if same {
				request = map[string]interface{}{"worker": input["worker"], "master": input["worker"]}
			}
			mutated, err := MutateWorkload(request, nil, same, nil, nil)
			require.Nil(t, err)
			worker, master, buildErr := buildResponseSpecFromMap[workloadsApi.SupersetSpec, workloadsApi.MasterSpec](Distributed, mutated)
			require.Nil(t, buildErr)
			require.Equal(t, workloadsApi.GPUCOMPUTEMODE_SM_SHARING, worker.Compute.Get().GetGpuComputeMode())
			expected := workloadsApi.GPUCOMPUTEMODE_TIME_SLICING
			if same {
				expected = workloadsApi.GPUCOMPUTEMODE_SM_SHARING
			}
			require.Equal(t, expected, master.Compute.Get().GetGpuComputeMode())
			if same {
				require.Nil(t, preValidateMpiGpuFields(context.Background(), worker, true, master))
				require.False(t, master.Compute.Get().GpuDevicesRequest.IsSet())
				require.Equal(t, workloadsApi.GPUCOMPUTEMODE_SM_SHARING, worker.Compute.Get().GetGpuComputeMode())
			}
		})
	}
}

func TestUnrelatedInferenceUpdateKeepsImplicitModeThroughPolicyNormalization(t *testing.T) {
	values := map[string]interface{}{"compute": map[string]interface{}{"gpuDevicesRequest": 1}}
	defaults := map[string]interface{}{"compute": map[string]interface{}{"gpuComputeMode": "sm-sharing"}}
	cleanup := gpumode.PreserveImplicitModeForUpdate(values)
	require.Nil(t, validateSpecAgainstPolicy(Inference, nil, defaults, values))
	cleanup()
	require.NotContains(t, values["compute"], "gpuComputeMode")
	var spec workloadsApi.SupersetSpec
	require.NoError(t, json.Unmarshal([]byte(`{"compute":{"gpuDevicesRequest":1}}`), &spec))
	require.NoError(t, copyResolvedGpuModes(&spec, values))
	require.False(t, spec.Compute.Get().GpuComputeMode.IsSet())
	data, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NotContains(t, string(data), `"computeMode"`)
	require.NotContains(t, string(data), `"gpuComputeMode"`)
}
