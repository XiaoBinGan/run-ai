/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package gpumode

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	leaderSection    = "leader"
	masterSection    = "master"
	workerSection    = "worker"
	TimeSlicing      = "time-slicing"
	SMSharing        = "sm-sharing"
	AnnotationFormat = "nvidia.com/container.%s.gpu-compute.mode"
)

// VisitCompute visits only workload compute sections, never arbitrary annotation,
// environment-variable or application configuration maps.
func VisitCompute(spec map[string]interface{}, visit func(map[string]interface{})) {
	if compute, ok := spec["compute"].(map[string]interface{}); ok {
		visit(compute)
	}
	for _, key := range []string{"spec", "masterSpec", workerSection, masterSection, leaderSection, "combinedSpec", "combinedMasterSpec"} {
		if child, ok := spec[key].(map[string]interface{}); ok {
			VisitCompute(child, visit)
		}
	}
}

func HasGPU(compute map[string]interface{}) bool {
	for _, key := range []string{"gpuDevicesRequest", "gpuMemoryPortionRequest", "gpuPortionRequest"} {
		value, err := strconv.ParseFloat(fmt.Sprint(compute[key]), 64)
		if err == nil && value > 0 {
			return true
		}
	}
	memory, ok := compute["gpuMemoryRequest"].(string)
	if !ok {
		return false
	}
	quantity, err := resource.ParseQuantity(memory)
	return err == nil && quantity.Sign() > 0
}

// PolicyFallback temporarily exposes the final fallback to policy constraints.
// Cleanup preserves omission so old clusters and CPU submissions retain their
// existing manifest; policy/defaultFrom values are never removed.
func PolicyFallback() (apply func(map[string]interface{}), cleanup func()) {
	var inserted []map[string]interface{}
	apply = func(spec map[string]interface{}) {
		VisitCompute(spec, func(compute map[string]interface{}) {
			if compute["gpuComputeMode"] == nil && HasGPU(compute) {
				compute["gpuComputeMode"] = TimeSlicing
				inserted = append(inserted, compute)
			}
		})
	}
	cleanup = func() {
		for _, compute := range inserted {
			delete(compute, "gpuComputeMode")
		}
	}
	return apply, cleanup
}

// ApplyModeDefaults fills only the new mode field during updates; existing fields
// retain the endpoint's established partial-update behavior.
func ApplyModeDefaults(values, defaults map[string]interface{}) {
	compute, ok := values["compute"].(map[string]interface{})
	defaultCompute, _ := defaults["compute"].(map[string]interface{})
	if ok && compute["gpuComputeMode"] == nil && defaultCompute["gpuComputeMode"] != nil {
		compute["gpuComputeMode"] = defaultCompute["gpuComputeMode"]
	}
	for _, key := range []string{workerSection, masterSection, leaderSection} {
		child, _ := values[key].(map[string]interface{})
		childDefaults, _ := defaults[key].(map[string]interface{})
		if child != nil {
			ApplyModeDefaults(child, childDefaults)
		}
	}
}

// ProjectResolvedModes copies only the mode selected by policy validation; other
// derived fields retain their existing update semantics.
func ProjectResolvedModes(resolved, submitted map[string]interface{}) {
	source, _ := resolved["compute"].(map[string]interface{})
	if target, ok := submitted["compute"].(map[string]interface{}); ok {
		delete(target, "gpuComputeMode")
		if mode := source["gpuComputeMode"]; mode != nil {
			target["gpuComputeMode"] = mode
		}
	}
	for _, key := range []string{workerSection, masterSection, leaderSection} {
		child, _ := submitted[key].(map[string]interface{})
		sourceChild, _ := resolved[key].(map[string]interface{})
		if child != nil {
			ProjectResolvedModes(sourceChild, child)
		}
	}
}

// PreserveImplicitModeForUpdate exposes the implicit time-slicing fallback to policy
// validation when compute is unchanged. Cleanup keeps the legacy request omitted
// so an unrelated update does not introduce a mode annotation or runtime gate.
func PreserveImplicitModeForUpdate(values map[string]interface{}) func() {
	compute, _ := values["compute"].(map[string]interface{})
	if !HasGPU(compute) || compute["gpuComputeMode"] != nil {
		return func() {}
	}
	compute["gpuComputeMode"] = TimeSlicing
	return func() {
		current, _ := values["compute"].(map[string]interface{})
		delete(current, "gpuComputeMode")
	}
}
