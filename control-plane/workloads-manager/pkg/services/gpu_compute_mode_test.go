/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services

import (
	"context"
	"encoding/json"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-manager/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-manager/pkg/middleware"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/convutils"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNimModeDefaultAndPersistence(t *testing.T) {
	var src api.NimServiceSpec
	require.NoError(t, json.Unmarshal([]byte(`{"compute":{"gpuMemoryPortionRequest":0.25}}`), &src))
	defaults := map[string]interface{}{"compute": map[string]interface{}{"gpuComputeMode": "sm-sharing"}}
	values, err := MutateWorkload(&src, nil, defaults)
	require.Nil(t, err)
	require.NotContains(t, values["compute"], "computeMode")
	_, err = validateMutatedSpecVsPolicy(nil, defaults, values)
	require.Nil(t, err)
	var resolved api.NimServiceSpec
	require.NoError(t, convutils.CopySpecs(&resolved, values))
	service := &NimServicesImpl{}
	ctx, ctxErr := service.updateContextWithMergedSpec(context.Background(), &api.NimServiceMetadataCreateParams{Name: "model", ProjectId: "p1"}, resolved)
	require.NoError(t, ctxErr)
	var saved api.NimServiceCreateRequest
	require.NoError(t, json.Unmarshal(middleware.GetRequestBody(ctx), &saved))
	require.NotContains(t, string(middleware.GetRequestBody(ctx)), `"computeMode"`)
	require.Equal(t, api.GPUCOMPUTEMODE_SM_SHARING, *saved.Spec.Compute.GpuComputeMode)
	require.Equal(t, api.GPUCOMPUTEMODE_SM_SHARING, *legacyCompatibleNimSpec(saved.Spec).Compute.GpuComputeMode)
}

func TestNimModeUpdatePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, mode, def string
		rule            map[string]interface{}
		fail            bool
	}{
		{name: "fallback"},
		{name: "default", def: "sm-sharing"},
		{name: "explicit", mode: "sm-sharing"},
		{name: "locked conflict", mode: "time-slicing", def: "sm-sharing", rule: map[string]interface{}{"canEdit": false}, fail: true},
		{name: "locked equal", mode: "sm-sharing", def: "sm-sharing", rule: map[string]interface{}{"canEdit": false}},
		{name: "fallback options", rule: map[string]interface{}{"options": []interface{}{map[string]interface{}{"value": "sm-sharing"}}}, fail: true},
		{name: "derived mode", rule: map[string]interface{}{"defaultFrom": map[string]interface{}{"field": "image"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var src api.NimServiceSpec
			require.NoError(t, json.Unmarshal([]byte(`{"image":"sm-sharing","compute":{"gpuMemoryPortionRequest":0.25}}`), &src))
			if tc.mode != "" {
				src.Compute.GpuComputeMode = convutils.Ref(api.GpuComputeMode(tc.mode))
			}
			defaults := map[string]interface{}{}
			if tc.def != "" {
				defaults["compute"] = map[string]interface{}{"gpuComputeMode": tc.def}
			}
			rules := map[string]interface{}{"compute": map[string]interface{}{"gpuComputeMode": tc.rule}, "image": map[string]interface{}{"blocked": true}}
			err := resolveNimGpuModeAgainstPolicy(&src, rules, defaults)
			if tc.fail {
				require.NotNil(t, err)
				require.Equal(t, int32(400), err.Code)
				return
			}
			require.Nil(t, err)
			expected := "time-slicing"
			if tc.mode != "" {
				expected = tc.mode
			} else if tc.def != "" || tc.name == "derived mode" {
				expected = "sm-sharing"
			}
			if tc.name == "fallback" {
				require.Nil(t, src.Compute.GpuComputeMode)
			} else {
				require.Equal(t, expected, string(*src.Compute.GpuComputeMode))
			}
			data, marshalErr := json.Marshal(src)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(data), `"computeMode"`)
		})
	}
}
