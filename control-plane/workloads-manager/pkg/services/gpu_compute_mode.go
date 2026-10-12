/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services

import (
	"context"
	"net/http"

	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/gpumode"
	policyCompliance "github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/policy_compliance"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-manager/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/convutils"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/jsonutils/gojsonq"
)

func (nsi *NimServicesImpl) resolveUpdatedNimGpuMode(ctx context.Context, projectID string, spec *api.NimServiceSpec) *api.Error {
	rules, defaults, _, err := GetPolicyOfProject(ctx, WorkloadTypeNimService, projectID)
	if err != nil {
		return err
	}
	return resolveNimGpuModeAgainstPolicy(spec, rules, defaults)
}

func resolveNimGpuModeAgainstPolicy(spec *api.NimServiceSpec, rules, defaults map[string]interface{}) *api.Error {
	values, loadErr := gojsonq.LoadJson(spec)
	if loadErr != nil {
		return api.NewError(http.StatusInternalServerError, "failed to load NIM spec", loadErr)
	}
	gpumode.ApplyModeDefaults(values, defaults)
	order, orderErr := policyCompliance.NewDefaultsFromOrder(rules)
	if orderErr != nil {
		return api.NewError(http.StatusBadRequest, orderErr.Error(), orderErr)
	}
	apply, cleanup := gpumode.PolicyFallback()
	fields := []string{}
	errors := policyCompliance.ComplianceErrors{}
	policyCompliance.PrepareDefaultsForMutation(defaults)
	policyCompliance.ValidateCompliance("", rules, defaults, values, order, &fields,
		&policyCompliance.ValidateComplianceOptions{ApplyBuiltInDefaults: apply}, &errors)
	cleanup()
	// NIM updates did not previously enforce policies. Keep existing fields'
	// update semantics while enforcing every rule on the new mode field.
	for _, issue := range errors {
		if issue.WorkloadField == "compute.gpuComputeMode" {
			return api.NewError(http.StatusBadRequest, "compute.gpuComputeMode does not comply with the policy: "+issue.Details, nil)
		}
	}
	submitted, loadErr := gojsonq.LoadJson(spec)
	if loadErr != nil {
		return api.NewError(http.StatusInternalServerError, "failed to read NIM GPU configuration", loadErr)
	}
	gpumode.ProjectResolvedModes(values, submitted)
	var resolved api.NimServiceSpec
	if err := convutils.CopySpecs(&resolved, submitted); err != nil {
		return api.NewError(http.StatusInternalServerError, "failed to resolve NIM compute mode", err)
	}
	if spec.Compute != nil && resolved.Compute != nil {
		compute := *spec.Compute
		compute.GpuComputeMode = resolved.Compute.GpuComputeMode
		spec.Compute = &compute
	}
	return nil
}
