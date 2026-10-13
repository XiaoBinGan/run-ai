/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services_crud

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/dsx-ai-factory/run-ai/control-plane/workloads-and-policies/policy_compliance"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/dao_crud"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/convutils"
	policyApi "github.com/dsx-ai-factory/run-ai/runai-clients/policy-service-client/generated"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GPU compute mode template compliance", func() {
	for _, replica := range []string{"", "Worker", "Master"} {
		DescribeTable("policy resolution for "+replica,
			func(mode, defaultMode, ruleJSON, source, wantRule string, hasGPU bool) {
				compute := map[string]interface{}{"gpuMemoryPortionRequest": 0.25}
				if !hasGPU {
					compute = map[string]interface{}{"cpuCoreRequest": 1.0}
				}
				if mode != "" {
					compute["gpuComputeMode"] = mode
				}
				values := map[string]interface{}{"compute": compute}
				specJSON, err := json.Marshal(values)
				Expect(err).NotTo(HaveOccurred())
				template := api.SupersetTemplate{Meta: api.TemplateMetaFields{Id: uuid.NewString()}}
				Expect(json.Unmarshal(specJSON, &template.CombinedSpec)).To(Succeed())
				if replica == "Master" {
					Expect(json.Unmarshal(specJSON, &template.CombinedMasterSpec)).To(Succeed())
				}
				before, err := json.Marshal(template)
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
				mutated, mutateErr := MutateWorkload(values, defaults, false, nil, nil)
				Expect(mutateErr).To(BeNil())
				submissionErr := validateSpecAgainstPolicy(Workspace, rules, defaults, mutated)
				Expect(submissionErr == nil).To(Equal(wantRule == ""))
				order, err := policy_compliance.NewDefaultsFromOrder(rules)
				Expect(err).NotTo(HaveOccurred())
				orderMap := map[string][]string(order)
				if replica != "" {
					rules = map[string]interface{}{strings.ToLower(replica): rules}
					defaults = map[string]interface{}{strings.ToLower(replica): defaults}
				}
				policyInfo := dao_crud.PolicyInfo{TenantId: 1, ProjectId: 1, WorkloadType: Workspace, ReplicaType: replica, PolicyTime: time.Now()}
				validateComplianceOfTemplate(context.Background(), patchingTaskInfo{
					template: &template, policyInfo: policyInfo,
					effectivePolicy: &policy_compliance.EffectivePolicyOfProject{
						RulesV2: rules, DefaultsV2: defaults,
						DefaultFromOrder: policyApi.DefaultFromOrder{Worker: &orderMap, Master: &orderMap},
					},
				})
				if wantRule == "" {
					Expect(template.ComplianceIssues).To(BeEmpty())
				} else {
					Expect(template.ComplianceIssues).To(HaveLen(1))
					Expect(template.ComplianceIssues[0].Field).To(Equal(convutils.Ref("compute.gpuComputeMode")))
					Expect(string(*template.ComplianceIssues[0].Rule)).To(Equal(wantRule))
				}
				saved, dbErr := dao_crud.ListPolicyComplianceOfTemplates(context.Background(), []string{template.Meta.Id}, policyInfo)
				Expect(dbErr).To(BeNil())
				Expect(saved).To(HaveLen(1))
				Expect(saved[0].Aligned).To(Equal(wantRule == ""))
				template.ComplianceIssues = nil
				after, err := json.Marshal(template)
				Expect(err).NotTo(HaveOccurred())
				Expect(after).To(Equal(before))
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
})
