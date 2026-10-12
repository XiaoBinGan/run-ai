/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services_crud

import (
	"fmt"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/gpumode"
	workloadsApi "github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/client/generated"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/convutils"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/jsonutils/gojsonq"
	"net/http"
)

// Inference updates historically validate other derived defaults without saving
// them. Project only mode resolution here, preserving that endpoint behavior.
func copyResolvedGpuModes(spec *workloadsApi.SupersetSpec, values map[string]interface{}) error {
	submitted, err := gojsonq.LoadJson(spec)
	if err != nil {
		return fmt.Errorf("read submitted GPU configuration: %w", err)
	}
	gpumode.ProjectResolvedModes(values, submitted)
	var resolved workloadsApi.SupersetSpec
	if err := convutils.CopySpecs(&resolved, submitted); err != nil {
		return fmt.Errorf("decode resolved GPU mode: %w", err)
	}
	*spec = resolved
	return nil
}

func validateNativeGpuMode(spec any) *workloadsApi.SubmissionErrorV2 {
	values, err := gojsonq.LoadJson(spec)
	if err != nil {
		return workloadsApi.NewSubmissionErrorV2(http.StatusInternalServerError, "failed to load GPU compute settings")
	}
	explicit := false
	gpumode.VisitCompute(values, func(compute map[string]interface{}) {
		if compute["gpuComputeMode"] != nil && gpumode.HasGPU(compute) {
			explicit = true
		}
	})
	if explicit {
		return workloadsApi.NewSubmissionErrorV2(http.StatusBadRequest, "compute.gpuComputeMode requires workloads v2 submission and an available GPU fractioning runtime")
	}
	return nil
}
