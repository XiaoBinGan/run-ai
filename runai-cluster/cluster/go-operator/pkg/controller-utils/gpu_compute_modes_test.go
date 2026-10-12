/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package controller_utils

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("GPU compute mode availability", func() {
	var (
		config          *unstructured.Unstructured
		fractiond, mpsd *appsv1.DaemonSet
		scheme          *runtime.Scheme
	)
	BeforeEach(func() {
		scheme = gpuFractioningTestScheme()
		Expect(appsv1.AddToScheme(scheme)).To(Succeed())
		config = newGpuFractioningConfig()
		config.SetUID("runtime-config")
		config.SetGeneration(2)
		conditions := []interface{}{}
		for _, conditionType := range []string{"Ready", "FractiondReady", "MpsdReady"} {
			conditions = append(conditions, map[string]interface{}{
				"type": conditionType, "status": "True", "observedGeneration": int64(2),
			})
		}
		Expect(unstructured.SetNestedSlice(config.Object, conditions, "status", "conditions")).To(Succeed())
		fractiond = gpuComputeTestDaemon(config, "fractiond")
		fractiond.Spec.Template.Spec.Containers[0].Args = []string{"--support-sm-sharing=true"}
		mpsd = gpuComputeTestDaemon(config, "mpsd")
		mpsd.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "MPS_SUPPORT_SM_SHARING", Value: "true"}}
	})
	check := func(objects ...client.Object) {
		k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		status := CheckGpuComputeModes(context.Background(), k8s)
		Expect(*status.Available).To(BeFalse())
		Expect(status.Reason).NotTo(BeEmpty())
		for _, mode := range []string{"time-slicing", "sm-sharing"} {
			Expect(*status.Components[mode].Available).To(BeFalse())
			Expect(status.Components[mode].Reason).NotTo(BeEmpty())
		}
	}

	It("reports both modes for a current, ready runtime with SM-sharing enabled", func() {
		k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config, fractiond, mpsd).Build()
		status := CheckGpuComputeModes(context.Background(), k8s)
		Expect(*status.Available).To(BeTrue())
		Expect(status.Reason).To(BeEmpty())
		Expect(*status.Components["time-slicing"].Available).To(BeTrue())
		Expect(*status.Components["sm-sharing"].Available).To(BeTrue())
		Expect(status.Components["sm-sharing"].Reason).To(BeEmpty())
	})

	DescribeTable("keeps time-slicing available when SM-sharing is not enabled",
		func(change func(*appsv1.DaemonSet, *appsv1.DaemonSet)) {
			change(fractiond, mpsd)
			k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config, fractiond, mpsd).Build()
			status := CheckGpuComputeModes(context.Background(), k8s)
			Expect(*status.Available).To(BeTrue())
			Expect(*status.Components["time-slicing"].Available).To(BeTrue())
			Expect(*status.Components["sm-sharing"].Available).To(BeFalse())
			Expect(status.Components["sm-sharing"].Reason).To(ContainSubstring("not enabled"))
		},
		Entry("fractiond disabled", func(f, m *appsv1.DaemonSet) {
			f.Spec.Template.Spec.Containers[0].Args = []string{"--support-sm-sharing=false"}
		}),
		Entry("conflicting fractiond flags", func(f, m *appsv1.DaemonSet) {
			f.Spec.Template.Spec.Containers[0].Args = []string{"--support-sm-sharing=true", "--support-sm-sharing=false"}
		}),
		Entry("conflicting mpsd settings", func(f, m *appsv1.DaemonSet) {
			m.Spec.Template.Spec.Containers[0].Env = append(m.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "MPS_SUPPORT_SM_SHARING", Value: "false"})
		}),
		Entry("fractiond flag absent", func(f, m *appsv1.DaemonSet) { f.Spec.Template.Spec.Containers[0].Args = nil }),
		Entry("mpsd disabled", func(f, m *appsv1.DaemonSet) { m.Spec.Template.Spec.Containers[0].Env[0].Value = "false" }),
		Entry("mpsd setting absent", func(f, m *appsv1.DaemonSet) { m.Spec.Template.Spec.Containers[0].Env = nil }),
		Entry("mpsd indirect setting", func(f, m *appsv1.DaemonSet) {
			m.Spec.Template.Spec.Containers[0].Env[0].ValueFrom = &corev1.EnvVarSource{}
		}),
	)

	DescribeTable("requires affirmative current conditions",
		func(conditionType, status string, generation int64) {
			conditions, _, err := unstructured.NestedSlice(config.Object, "status", "conditions")
			Expect(err).NotTo(HaveOccurred())
			for _, raw := range conditions {
				condition := raw.(map[string]interface{})
				if condition["type"] == conditionType {
					condition["status"] = status
					condition["observedGeneration"] = generation
				}
			}
			Expect(unstructured.SetNestedSlice(config.Object, conditions, "status", "conditions")).To(Succeed())
			check(config, fractiond, mpsd)
		},
		Entry("Ready false", "Ready", "False", int64(2)),
		Entry("Ready unknown", "Ready", "Unknown", int64(2)),
		Entry("Ready invalid", "Ready", "invalid", int64(2)),
		Entry("Ready stale", "Ready", "True", int64(1)),
		Entry("Ready without observed generation", "Ready", "True", int64(0)),
		Entry("fractiond unready", "FractiondReady", "False", int64(2)),
		Entry("mpsd unready", "MpsdReady", "False", int64(2)),
		Entry("mpsd stale", "MpsdReady", "True", int64(1)),
	)

	It("rejects missing readiness evidence while preserving legacy readiness", func() {
		unstructured.RemoveNestedField(config.Object, "status")
		k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config, fractiond, mpsd).Build()
		Expect(*CheckGpuFractioningReadiness(context.Background(), k8s).Available).To(BeTrue())
		check(config, fractiond, mpsd)
	})
	It("rejects a missing runtime without changing legacy GPU-stack availability", func() {
		k8s := fake.NewClientBuilder().WithScheme(scheme).Build()
		Expect(*CheckGpuFractioningReadiness(context.Background(), k8s).Available).To(BeTrue())
		check()
	})
	It("rejects a runtime being deleted", func() {
		config.SetFinalizers([]string{"cleanup"})
		config.SetDeletionTimestamp(ptr.To(metav1.Now()))
		check(config, fractiond, mpsd)
	})
	It("requires the standard annotation prefix", func() {
		Expect(unstructured.SetNestedField(config.Object, "example.com/container.", "spec", "fractioningAgent", "annotationPrefix")).To(Succeed())
		check(config, fractiond, mpsd)
	})
	It("accepts an explicit standard annotation prefix", func() {
		Expect(unstructured.SetNestedField(config.Object, "nvidia.com/container.", "spec", "fractioningAgent", "annotationPrefix")).To(Succeed())
		k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config, fractiond, mpsd).Build()
		Expect(*CheckGpuComputeModes(context.Background(), k8s).Available).To(BeTrue())
	})

	DescribeTable("requires an owned and fully rolled out daemon",
		func(change func(*appsv1.DaemonSet)) {
			change(fractiond)
			check(config, fractiond, mpsd)
		},
		Entry("no target nodes despite Ready=True", func(d *appsv1.DaemonSet) { d.Status.DesiredNumberScheduled = 0 }),
		Entry("stale observation", func(d *appsv1.DaemonSet) { d.Status.ObservedGeneration-- }),
		Entry("old pods still running", func(d *appsv1.DaemonSet) { d.Status.UpdatedNumberScheduled-- }),
		Entry("unready pods", func(d *appsv1.DaemonSet) { d.Status.NumberReady-- }),
		Entry("pods not available yet", func(d *appsv1.DaemonSet) { d.Status.NumberAvailable-- }),
		Entry("deletion", func(d *appsv1.DaemonSet) {
			d.Finalizers = []string{"cleanup"}
			d.DeletionTimestamp = ptr.To(metav1.Now())
		}),
		Entry("unmanaged lookalike", func(d *appsv1.DaemonSet) { delete(d.Labels, "app.kubernetes.io/managed-by") }),
		Entry("no owner", func(d *appsv1.DaemonSet) { d.OwnerReferences = nil }),
		Entry("old config owner", func(d *appsv1.DaemonSet) { d.OwnerReferences[0].UID = "old-config" }),
		Entry("other namespace", func(d *appsv1.DaemonSet) { d.Namespace = "other-runtime" }),
		Entry("missing container", func(d *appsv1.DaemonSet) { d.Spec.Template.Spec.Containers = nil }),
	)
	It("requires mpsd even for time-slicing", func() { check(config, fractiond) })
	It("rejects ambiguous daemon installations", func() {
		duplicate := fractiond.DeepCopy()
		duplicate.Name = "duplicate"
		check(config, fractiond, mpsd, duplicate)
	})
	DescribeTable("does not treat API read errors as support", func(operation string) {
		funcs := interceptor.Funcs{}
		if operation == "get" {
			funcs.Get = func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return errors.New("forbidden")
			}
		} else {
			funcs.List = func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("forbidden")
			}
		}
		k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(config, fractiond, mpsd).WithInterceptorFuncs(funcs).Build()
		status := CheckGpuComputeModes(context.Background(), k8s)
		Expect(*status.Available).To(BeFalse())
		Expect(status.Reason).To(ContainSubstring("forbidden"))
	}, Entry("configuration", "get"), Entry("daemons", "list"))
})

func gpuComputeTestDaemon(config *unstructured.Unstructured, component string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-fractioning-" + component, Namespace: "runtime", Generation: 3,
			Labels:          map[string]string{"app.kubernetes.io/managed-by": "gpu-fractioning", "app.kubernetes.io/component": component},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(config, gpuFractioningConfigGVK)},
		},
		Spec:   appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: component}}}}},
		Status: appsv1.DaemonSetStatus{ObservedGeneration: 3, DesiredNumberScheduled: 2, UpdatedNumberScheduled: 2, NumberReady: 2, NumberAvailable: 2},
	}
}
