/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package controller_utils

import (
	"context"
	"fmt"

	runv1 "github.com/dsx-ai-factory/run-ai/runai-cluster/cluster/sdk/apis/run/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	gpuFractioningAppNameLabel = "app.kubernetes.io/name"
	gpuFractioningReleaseLabel = "app.kubernetes.io/instance"
)

// CheckGpuComputeModes reports configured compute modes independently of runtime
// health. gpu-fractioning v0.1.7 configures both daemons from its operator's
// SUPPORT_SM_SHARING setting; the CR does not expose that setting.
func CheckGpuComputeModes(ctx context.Context, reader client.Reader) runv1.PublicStatus {
	unavailable := func(reason string) runv1.PublicStatus {
		status := runv1.PublicStatus{Available: runv1.AvailableStatus(false), Reason: reason}
		return runv1.PublicStatus{
			Available: runv1.AvailableStatus(false), Reason: reason,
			Components: map[string]runv1.PublicStatus{"time-slicing": status, "sm-sharing": status},
		}
	}

	config := &unstructured.Unstructured{}
	config.SetGroupVersionKind(gpuFractioningConfigGVK)
	if err := reader.Get(ctx, types.NamespacedName{Name: gpuFractioningConfigName}, config); err != nil {
		return unavailable(fmt.Sprintf("unable to read GPU fractioning configuration: %v", err))
	}
	if config.GetDeletionTimestamp() != nil {
		return unavailable("GPU fractioning configuration is being deleted")
	}
	prefix, _, err := unstructured.NestedString(config.Object, "spec", "fractioningAgent", "annotationPrefix")
	if err != nil || (prefix != "" && prefix != "nvidia.com/container.") {
		return unavailable("GPU fractioning uses an unsupported annotation prefix")
	}

	operator, reason := gpuComputeOperator(ctx, reader, config)
	if reason != "" {
		return unavailable(reason)
	}
	smSharing, reason := gpuComputeSMSharingEnabled(operator)
	return runv1.PublicStatus{
		Available: runv1.AvailableStatus(true),
		Components: map[string]runv1.PublicStatus{
			"time-slicing": {Available: runv1.AvailableStatus(true)},
			"sm-sharing":   {Available: runv1.AvailableStatus(smSharing), Reason: reason},
		},
	}
}

func gpuComputeOperator(ctx context.Context, reader client.Reader, config *unstructured.Unstructured) (*corev1.Container, string) {
	// Both chart objects carry these labels, including name/release overrides.
	// The operator Deployment is Helm-managed, not owned by the runtime CR.
	selector := client.MatchingLabels{}
	for _, key := range []string{gpuFractioningAppNameLabel, gpuFractioningReleaseLabel} {
		value := config.GetLabels()[key]
		if value == "" {
			return nil, "GPU fractioning configuration is missing installation labels"
		}
		selector[key] = value
	}
	operators := &appsv1.DeploymentList{}
	if err := reader.List(ctx, operators, selector); err != nil {
		return nil, fmt.Sprintf("unable to read GPU fractioning operator: %v", err)
	}
	if len(operators.Items) != 1 {
		return nil, fmt.Sprintf("expected one GPU fractioning operator, found %d", len(operators.Items))
	}
	operator := &operators.Items[0]
	if !operator.DeletionTimestamp.IsZero() {
		return nil, "GPU fractioning operator is being deleted"
	}
	for i := range operator.Spec.Template.Spec.Containers {
		container := &operator.Spec.Template.Spec.Containers[i]
		if container.Name == "manager" {
			return container, ""
		}
	}
	return nil, "GPU fractioning operator manager container is missing"
}

func gpuComputeSMSharingEnabled(operator *corev1.Container) (bool, string) {
	var setting *corev1.EnvVar
	for i := range operator.Env {
		if operator.Env[i].Name != "SUPPORT_SM_SHARING" {
			continue
		}
		if setting != nil {
			return false, "GPU fractioning operator has ambiguous SUPPORT_SM_SHARING settings"
		}
		setting = &operator.Env[i]
	}
	// The bundled chart always writes a literal value. Missing or indirect
	// configuration cannot establish support, even though the binary defaults on.
	if setting == nil || setting.ValueFrom != nil {
		return false, "GPU fractioning operator SUPPORT_SM_SHARING setting cannot be determined"
	}
	switch setting.Value {
	case "true":
		return true, ""
	case "false":
		return false, "SM-sharing is disabled in the GPU fractioning operator"
	default:
		return false, "GPU fractioning operator SUPPORT_SM_SHARING setting is invalid"
	}
}
