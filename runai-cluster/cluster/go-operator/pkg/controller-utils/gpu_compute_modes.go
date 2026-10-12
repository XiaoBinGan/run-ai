/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package controller_utils

import (
	"context"
	"fmt"
	"strings"

	runv1 "github.com/dsx-ai-factory/run-ai/runai-cluster/cluster/sdk/apis/run/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CheckGpuComputeModes reports readiness for explicit compute-mode requests.
// Unlike the legacy GPU-stack check, missing runtime evidence is not sufficient
// to advertise support. The DaemonSet contract is from gpu-fractioning v0.1.5.
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
	conditions, found, err := unstructured.NestedSlice(config.Object, "status", "conditions")
	if err != nil || !found {
		return unavailable("GPU fractioning readiness has not been reported")
	}
	// Ready can be true with no target nodes. DaemonSet readiness below also
	// requires running, updated instances of both runtime components.
	for _, conditionType := range []string{"Ready", "FractiondReady", "MpsdReady"} {
		if reason := gpuComputeConditionReason(conditions, conditionType, config.GetGeneration()); reason != "" {
			return unavailable(reason)
		}
	}
	prefix, _, err := unstructured.NestedString(config.Object, "spec", "fractioningAgent", "annotationPrefix")
	if err != nil || (prefix != "" && prefix != "nvidia.com/container.") {
		return unavailable("GPU fractioning uses an unsupported annotation prefix")
	}

	daemons := &appsv1.DaemonSetList{}
	if err := reader.List(ctx, daemons, client.MatchingLabels{"app.kubernetes.io/managed-by": "gpu-fractioning"}); err != nil {
		return unavailable(fmt.Sprintf("unable to read GPU fractioning daemons: %v", err))
	}
	fractiond, reason := gpuComputeDaemon(daemons, config, "fractiond")
	if reason != "" {
		return unavailable(reason)
	}
	mpsd, reason := gpuComputeDaemon(daemons, config, "mpsd")
	if reason != "" {
		return unavailable(reason)
	}
	if fractiond.Namespace != mpsd.Namespace {
		return unavailable("GPU fractioning daemons are in different namespaces")
	}

	fractiondContainer := gpuComputeContainer(fractiond, "fractiond")
	mpsdContainer := gpuComputeContainer(mpsd, "mpsd")
	if fractiondContainer == nil || mpsdContainer == nil {
		return unavailable("GPU fractioning daemon containers are missing")
	}
	smSharing := gpuComputeSMSharingEnabled(fractiondContainer, mpsdContainer)
	smStatus := runv1.PublicStatus{Available: runv1.AvailableStatus(smSharing)}
	if !smSharing {
		smStatus.Reason = "SM-sharing is not enabled in both GPU fractioning daemons"
	}
	return runv1.PublicStatus{
		Available: runv1.AvailableStatus(true),
		Components: map[string]runv1.PublicStatus{
			"time-slicing": {Available: runv1.AvailableStatus(true)},
			"sm-sharing":   smStatus,
		},
	}
}

func gpuComputeConditionReason(conditions []interface{}, conditionType string, generation int64) string {
	for _, raw := range conditions {
		fields, ok := raw.(map[string]interface{})
		if !ok || fields["type"] != conditionType {
			continue
		}
		condition := &metav1.Condition{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(fields, condition); err != nil {
			return fmt.Sprintf("GPU fractioning condition %s is invalid", conditionType)
		}
		if generation < 1 || condition.ObservedGeneration != generation {
			return fmt.Sprintf("GPU fractioning condition %s is not current", conditionType)
		}
		if condition.Status != metav1.ConditionTrue {
			return fmt.Sprintf("GPU fractioning condition %s is not ready: %s", conditionType, condition.Message)
		}
		return ""
	}
	return fmt.Sprintf("GPU fractioning condition %s has not been reported", conditionType)
}

func gpuComputeDaemon(daemons *appsv1.DaemonSetList, config *unstructured.Unstructured, component string) (*appsv1.DaemonSet, string) {
	var result *appsv1.DaemonSet
	for i := range daemons.Items {
		daemon := &daemons.Items[i]
		owner := metav1.GetControllerOf(daemon)
		if daemon.Labels["app.kubernetes.io/component"] != component || owner == nil ||
			owner.UID != config.GetUID() || owner.Name != config.GetName() ||
			owner.APIVersion != config.GetAPIVersion() || owner.Kind != config.GetKind() {
			continue
		}
		if result != nil {
			return nil, fmt.Sprintf("multiple GPU fractioning %s daemons found", component)
		}
		result = daemon
	}
	if result == nil {
		return nil, fmt.Sprintf("GPU fractioning %s daemon is missing", component)
	}
	status := result.Status
	if !result.DeletionTimestamp.IsZero() || result.Generation < 1 || result.Generation != status.ObservedGeneration ||
		status.DesiredNumberScheduled == 0 || status.UpdatedNumberScheduled != status.DesiredNumberScheduled ||
		status.NumberReady != status.DesiredNumberScheduled || status.NumberAvailable != status.DesiredNumberScheduled {
		return nil, fmt.Sprintf("GPU fractioning %s daemon has no fully ready rollout", component)
	}
	return result, ""
}

func gpuComputeContainer(daemon *appsv1.DaemonSet, name string) *corev1.Container {
	for i := range daemon.Spec.Template.Spec.Containers {
		if daemon.Spec.Template.Spec.Containers[i].Name == name {
			return &daemon.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

func gpuComputeSMSharingEnabled(fractiond, mpsd *corev1.Container) bool {
	// The pinned operator emits one explicit setting per daemon. Ambiguous or
	// indirect settings cannot establish support from the DaemonSet template.
	flags, settings := 0, 0
	enabled := true
	for _, arg := range fractiond.Args {
		if strings.HasPrefix(arg, "--support-sm-sharing") {
			flags++
			enabled = enabled && arg == "--support-sm-sharing=true"
		}
	}
	for _, env := range mpsd.Env {
		if env.Name == "MPS_SUPPORT_SM_SHARING" {
			settings++
			enabled = enabled && env.ValueFrom == nil && env.Value == "true"
		}
	}
	return enabled && flags == 1 && settings == 1
}
