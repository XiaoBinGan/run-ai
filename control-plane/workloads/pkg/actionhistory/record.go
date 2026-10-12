/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package actionhistory

import (
	"context"
	"fmt"
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

// WorkloadActionRecord builds a history record from the authenticated request context.
func WorkloadActionRecord(ctx context.Context, action api.WorkloadActionType, workload WorkloadToLog, acceptedAt time.Time) (*models.WorkloadActionHistory, error) {
	if ctx == nil {
		return nil, fmt.Errorf("missing authenticated context")
	}
	tenantID, err := tenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	subjectID, subjectType, err := subjectFromContext(ctx)
	if err != nil {
		return nil, err
	}
	recordID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("generate workload action id: %w", err)
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

func tenantIDFromContext(ctx context.Context) (uint, error) {
	tenant, err := runaictx.ParseTenantId(ctx)
	if err != nil {
		return 0, fmt.Errorf("read action tenant: %w", err)
	}
	// PostgreSQL bigint is signed.
	tenantID, err := strconv.ParseUint(tenant, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("parse action tenant: %w", err)
	}
	return uint(tenantID), nil
}

func subjectFromContext(ctx context.Context) (string, string, error) {
	subjectID, err := runaictx.ParseSubjectId(ctx)
	if err != nil {
		return "", "", fmt.Errorf("read action subject: %w", err)
	}
	if strings.TrimSpace(subjectID) == "" {
		return "", "", fmt.Errorf("missing action subject")
	}
	subjectType := runaictx.GetString(ctx, runaictx.CfSubjectType)
	if subjectType == nil || strings.TrimSpace(*subjectType) == "" {
		return "", "", fmt.Errorf("missing action subject type")
	}
	return subjectID, *subjectType, nil
}
