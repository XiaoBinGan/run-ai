/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package inference_workloads

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/composer"
	workloadsApi "github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/client/generated"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("GPU mode annotation projection", func() {
	DescribeTable("targets the GPU container and preserves the memory request", func(mode string) {
		options := composer.FieldsComposerOptions{GpuComputeModeStatus: func(context.Context, string) (bool, string, error) { return true, "", nil }}
		var src workloadsApi.WorkspaceSpecSpecAllOfCompute
		Expect(json.Unmarshal([]byte(fmt.Sprintf(`{"gpuMemoryPortionRequest":0.25,"gpuComputeMode":%q}`, mode)), &src)).To(Succeed())
		c := NewInferenceToKnativeComposer("example", "p1", "runai-p1", "user", nil, options)
		target := NewMinimalKnativeServiceTarget()
		target.Service.Spec.Template.Spec.Containers = []corev1.Container{{Name: "inference-main"}, {Name: "auth-proxy"}}
		Expect(c.processComputeFields(context.Background(), &src, target)()).To(BeNil())
		Expect(target.Service.Spec.Template.Annotations).To(HaveKeyWithValue("nvidia.com/container.inference-main.gpu-compute.mode", mode))
		Expect(target.Service.Spec.Template.Annotations).NotTo(HaveKey("nvidia.com/container.auth-proxy.gpu-compute.mode"))
		Expect(target.Service.Spec.Template.Annotations).To(HaveKeyWithValue(composer.GpuFractionAnnotation, "0.25"))
	}, Entry("time slicing", "time-slicing"), Entry("SM sharing", "sm-sharing"))
})
