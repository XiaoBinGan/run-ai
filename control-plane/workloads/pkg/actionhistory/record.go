/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package actionhistory

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/models"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/runaictx"
	"github.com/google/uuid"
)

type WorkloadToLog struct {
	ID           uuid.UUID
	ClusterID    uuid.UUID
	ProjectID    string
	DepartmentID string
	Name         string
}

// NewWorkloadActionRecord builds a history record from the authenticated request context.
func NewWorkloadActionRecord(ctx context.Context, action api.WorkloadActionType, workload WorkloadToLog, acceptedAt time.Time) (*models.WorkloadActionHistory, *api.Error) {
	if ctx == nil {
		return nil, api.NewValidationError("missing authenticated context")
	}
	tenantID, apiErr := tenantIDFromContext(ctx)
	if apiErr != nil {
		return nil, apiErr
	}
	subjectID, subjectType, apiErr := subjectFromContext(ctx)
	if apiErr != nil {
		return nil, apiErr
	}
	recordID, err := uuid.NewRandom()
	if err != nil {
		return nil, api.NewError(http.StatusInternalServerError, "failed to generate workload action ID", err)
	}
	return &models.WorkloadActionHistory{
		ID: recordID,
		WorkloadActionScope: models.WorkloadActionScope{
			TenantID:     tenantID,
			ClusterID:    workload.ClusterID,
			ProjectID:    workload.ProjectID,
			DepartmentID: workload.DepartmentID,
		},
		WorkloadID:   workload.ID,
		WorkloadName: workload.Name,
		Subject:      subjectID,
		// Examples: "User"/"user" for a person; "ServiceAccount"/"service-account" for an application.
		// Older identity labels include "App"/"app". Preserve the authenticated value unchanged.
		SubjectType: subjectType,
		Action:      string(action),
		AcceptedAt:  acceptedAt.UTC().Truncate(time.Microsecond),
	}, nil
}

func tenantIDFromContext(ctx context.Context) (uint, *api.Error) {
	tenant, err := runaictx.ParseTenantId(ctx)
	if err != nil {
		return 0, api.NewError(http.StatusBadRequest, "failed to read action tenant", err)
	}
	// PostgreSQL bigint is signed.
	tenantID, err := strconv.ParseUint(tenant, 10, 63)
	if err != nil {
		return 0, api.NewError(http.StatusBadRequest, "invalid action tenant", err)
	}
	return uint(tenantID), nil
}

func subjectFromContext(ctx context.Context) (string, string, *api.Error) {
	subjectID, err := runaictx.ParseSubjectId(ctx)
	if err != nil {
		return "", "", api.NewError(http.StatusBadRequest, "failed to read action subject", err)
	}
	if strings.TrimSpace(subjectID) == "" {
		return "", "", api.NewValidationError("missing action subject")
	}
	subjectType := runaictx.GetString(ctx, runaictx.CfSubjectType)
	if subjectType == nil || strings.TrimSpace(*subjectType) == "" {
		return "", "", api.NewValidationError("missing action subject type")
	}
	return subjectID, *subjectType, nil
}
