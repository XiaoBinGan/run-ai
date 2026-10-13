/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package flat_workloads

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/dsx-ai-factory/run-ai/control-plane/assets-service/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/assets-service/pkg/database/dao"
	"github.com/dsx-ai-factory/run-ai/control-plane/assets-service/pkg/services/response_patching"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/auth"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/policy_compliance"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/convutils"
	policyApi "github.com/dsx-ai-factory/run-ai/runai-clients/policy-service-client/generated"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GPU compute mode asset compliance", func() {
	for _, replica := range []string{"", "Worker", "Master", "Leader"} {
		DescribeTable("policy resolution for "+replica,
			func(mode, defaultMode, ruleJSON, source, wantRule string, hasGPU bool) {
				spec := map[string]interface{}{"gpuMemoryPortionRequest": 0.25}
				if !hasGPU {
					spec = map[string]interface{}{"cpuCoreRequest": 1.0}
				}
				if mode != "" {
					spec["gpuComputeMode"] = mode
				}
				before, err := json.Marshal(spec)
				Expect(err).NotTo(HaveOccurred())
				var modeRules map[string]interface{}
				Expect(json.Unmarshal([]byte(ruleJSON), &modeRules)).To(Succeed())
				rules := map[string]interface{}{"compute": map[string]interface{}{"gpuComputeMode": modeRules}}
				defaults := map[string]interface{}{}
				if defaultMode != "" {
					defaults["compute"] = map[string]interface{}{"gpuComputeMode": defaultMode}
				}
				if source != "" {
					defaults["image"] = source
				}
				order, err := policy_compliance.NewDefaultsFromOrder(rules)
				Expect(err).NotTo(HaveOccurred())
				orderMap := map[string][]string(order)
				if replica != "" {
					rules = map[string]interface{}{strings.ToLower(replica): rules}
					defaults = map[string]interface{}{strings.ToLower(replica): defaults}
				}
				meta := api.AssetMeta{Id: uuid.NewString(), Name: "mode-compliance", Kind: api.ASSETKIND_COMPUTE}
				compliance := &api.ComplianceInfo{Compliance: true, Imposed: true}
				policyInfo := dao.PolicyInfo{TenantId: 1, ProjectId: 1, WorkloadType: auth.WorkloadTypeWorkspace, ReplicaType: replica, PolicyTime: time.Now()}
				info := assetChanInfo{
					patchingPtrs: response_patching.ResponsePatchingPtrs{Meta: &meta, Spec: &spec, ComplianceInfo: &compliance},
					rules:        rules, defaults: defaults, replicaType: replica,
					defaultFromOrder: policyApi.DefaultFromOrder{Worker: &orderMap, Master: &orderMap},
					workloadType:     auth.WorkloadTypeWorkspace, policyInfo: policyInfo, isImposed: true,
				}
				validateComplianceOfAsset(context.Background(), info)
				Expect(compliance.Compliance).To(Equal(wantRule == ""))
				Expect(compliance.Imposed).To(Equal(wantRule == ""))
				if wantRule != "" {
					Expect(compliance.Reason).To(HaveLen(1))
					Expect(compliance.Reason[0].Field).To(Equal(convutils.Ref("compute.gpuComputeMode")))
					Expect(string(*compliance.Reason[0].Rule)).To(Equal(wantRule))
				}
				after, err := json.Marshal(spec)
				Expect(err).NotTo(HaveOccurred())
				Expect(after).To(Equal(before))
				saved, dbErr := dao.ListPolicyComplianceOfAssets(context.Background(), []string{meta.Id}, policyInfo)
				Expect(dbErr).To(BeNil())
				Expect(saved).To(HaveLen(1))
				Expect(saved[0].Aligned).To(Equal(wantRule == ""))
			},

			Entry("implicit mode satisfies allowed time-slicing", "", "", `{"options":[{"value":"time-slicing"}]}`, "", "", true),
			Entry("implicit mode violates allowed SM-sharing", "", "", `{"options":[{"value":"sm-sharing"}]}`, "", "options", true),
			Entry("policy default precedes fallback", "", "sm-sharing", `{"options":[{"value":"sm-sharing"}]}`, "", "", true),
			Entry("editable explicit mode wins", "time-slicing", "sm-sharing", `{}`, "", "", true),
			Entry("locked policy default fills omission", "", "sm-sharing", `{"canEdit":false}`, "", "", true),
			Entry("locked equal mode passes", "sm-sharing", "sm-sharing", `{"canEdit":false}`, "", "", true),
			Entry("locked conflicting mode fails", "time-slicing", "sm-sharing", `{"canEdit":false}`, "", "canEdit", true),
			Entry("implicit mode satisfies required", "", "", `{"required":true}`, "", "", true),
			Entry("implicit mode violates blocked", "", "", `{"blocked":true}`, "", "blocked", true),
			Entry("derivation precedes fallback", "", "", `{"defaultFrom":{"field":"image"},"options":[{"value":"sm-sharing"}]}`, "sm-sharing", "", true),
			Entry("ordinary default precedes derivation", "", "sm-sharing", `{"defaultFrom":{"field":"image"},"options":[{"value":"sm-sharing"}]}`, "time-slicing", "", true),
			Entry("explicit mode precedes derivation", "time-slicing", "", `{"defaultFrom":{"field":"image"},"options":[{"value":"time-slicing"}]}`, "sm-sharing", "", true),
			Entry("CPU-only spec has no implicit GPU mode", "", "", `{"options":[{"value":"sm-sharing"}]}`, "", "", false),
			Entry("CPU-only required mode follows ordinary rules", "", "", `{"required":true}`, "", "required", false),
		)
	}

	It("replaces cached compliance when memory aliases conflict", func() {
		spec := map[string]interface{}{"gpuPortionRequest": 0.25, "gpuMemoryPortionRequest": 0.5}
		meta := api.AssetMeta{Id: uuid.NewString(), Name: "conflicting-memory", Kind: api.ASSETKIND_COMPUTE}
		compliance := &api.ComplianceInfo{Compliance: true, Imposed: true}
		policyInfo := dao.PolicyInfo{TenantId: 1, ProjectId: 1, WorkloadType: auth.WorkloadTypeWorkspace, PolicyTime: time.Now()}
		Expect(dao.UpsertPolicyCompliance(context.Background(), meta.Id, policyInfo, *compliance)).To(BeNil())
		validateComplianceOfAsset(context.Background(), assetChanInfo{
			patchingPtrs: response_patching.ResponsePatchingPtrs{Meta: &meta, Spec: &spec, ComplianceInfo: &compliance},
			workloadType: auth.WorkloadTypeWorkspace, policyInfo: policyInfo, isImposed: true,
		})
		Expect(compliance.Compliance).To(BeFalse())
		Expect(compliance.Imposed).To(BeFalse())
		Expect(compliance.Reason).To(HaveLen(1))
		Expect(compliance.Reason[0].Details).To(ContainSubstring("conflict"))
		saved, dbErr := dao.ListPolicyComplianceOfAssets(context.Background(), []string{meta.Id}, policyInfo)
		Expect(dbErr).To(BeNil())
		Expect(saved).To(HaveLen(1))
		Expect(saved[0].Aligned).To(BeFalse())
		Expect(saved[0].ImposedAsset).To(BeFalse())
		Expect(saved[0].Reason).To(Equal(compliance.Reason))
	})
})
