/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services

import (
	"context"
	"net/http"
	"strconv"

	authorization "github.com/dsx-ai-factory/run-ai/control-plane/authorization/pkg/client"
	authorization_utils "github.com/dsx-ai-factory/run-ai/control-plane/authorization/pkg/client/helpers"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/dao"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/models"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/services_utils"
	"github.com/google/uuid"
	"k8s.io/utils/ptr"
)

func (s *EventsService) GetWorkloadActions(ctx context.Context, workloadId string, offset, limit int32) (api.ImplResponse, error) {
	tenantID, apiErr := services_utils.ParseTenantId(ctx)
	if apiErr != nil {
		return NewErrorResponse(apiErr), nil
	}
	id, err := uuid.Parse(workloadId)
	if err != nil {
		return NewErrorResponse(api.NewValidationError("workload Id must be a valid uuid")), nil
	}
	if offset < 0 || limit < 0 {
		return NewErrorResponse(api.NewValidationError("offset and limit must not be negative")), nil
	}
	if apiErr := authorizeWorkloadActionsRead(ctx, tenantID, id); apiErr != nil {
		return NewErrorResponse(apiErr), nil
	}
	limit = setDefaultLimit(limit)
	records, dbErr := dao.DbGetWorkloadActions(ctx, tenantID, id, offset, limit)
	if dbErr != nil {
		return NewErrorResponse(dbErr), nil
	}
	response := buildWorkloadActionsResponse(records, offset, limit)
	return api.Response(http.StatusOK, response), nil
}

func authorizeWorkloadActionsRead(ctx context.Context, tenantID uint, workloadID uuid.UUID) *api.Error {
	scopes, scopesErr := authorization.ParsePermittedScopes(ctx)
	if scopesErr != nil {
		return api.NewError(http.StatusForbidden, "Workload read is not permitted", nil)
	}
	ancestors := &authorization_utils.Ancestors{TenantId: strconv.FormatUint(uint64(tenantID), 10)}
	scope, scopeErr := dao.DbGetWorkloadActionScope(ctx, tenantID, workloadID)
	if scopeErr != nil && scopeErr.Code != http.StatusNotFound {
		return scopeErr
	}
	if scope != nil {
		ancestors.ClusterUuid = ptr.To(scope.ClusterID.String())
		ancestors.ProjectId = ptr.To(scope.ProjectID)
		ancestors.DepartmentId = ptr.To(scope.DepartmentID)
	}
	if !authorization_utils.IsActionOnResourcePermittedByScopes(ancestors, scopes) {
		return api.NewError(http.StatusForbidden, "Workload read is not permitted", nil)
	}
	return nil
}

func buildWorkloadActionsResponse(records []models.WorkloadActionHistory, offset, limit int32) api.GetWorkloadActionsResponse {
	response := api.GetWorkloadActionsResponse{Actions: []api.WorkloadActionRecord{}}
	for _, record := range records {
		response.Actions = append(response.Actions, api.WorkloadActionRecord{
			Id:           record.ID.String(),
			WorkloadId:   record.WorkloadID.String(),
			WorkloadName: record.WorkloadName,
			Subject:      record.Subject,
			SubjectType:  record.SubjectType,
			Action:       api.WorkloadActionType(record.Action),
			AcceptedAt:   record.AcceptedAt,
		})
	}
	if int32(len(records)) == limit {
		response.Next = offset + limit
	}
	return response
}
