/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package dao

import (
	"context"
	"net/http"

	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/models"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/runaidb"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/runaidb/orm/clause"
	"github.com/google/uuid"
)

const (
	defaultWorkloadActionsLimit = 50
	maxWorkloadActionsLimit     = 500
)

func DbInsertWorkloadActions(ctx context.Context, records []models.WorkloadActionHistory) *api.Error {
	if len(records) == 0 {
		return nil
	}
	_, err := runaidb.DB().WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&records)
	if err != nil {
		return api.NewError(http.StatusInternalServerError, "Failed to insert workload action history", err)
	}
	return nil
}

func DbGetWorkloadActions(ctx context.Context, tenantID uint, workloadID uuid.UUID, offset, limit int32) ([]models.WorkloadActionHistory, *api.Error) {
	records := []models.WorkloadActionHistory{}
	if offset < 0 || limit < 0 {
		return records, api.NewValidationError("offset and limit must not be negative")
	}
	if limit == 0 {
		limit = defaultWorkloadActionsLimit
	}
	if limit > maxWorkloadActionsLimit {
		limit = maxWorkloadActionsLimit
	}
	err := runaidb.DB().WithContext(ctx).
		Where("tenant_id = ? AND workload_id = ?", tenantID, workloadID).
		Order("accepted_at DESC, id DESC").
		Offset(int(offset)).
		Limit(int(limit)).
		Find(&records)
	if err != nil {
		return records, api.NewError(http.StatusInternalServerError, "Failed to list workload actions", err)
	}
	return records, nil
}

// Use retained scope only when the workload row no longer exists.
func DbGetWorkloadActionScope(ctx context.Context, tenantID uint, workloadID uuid.UUID) (*models.WorkloadActionScope, *api.Error) {
	scopes := []models.WorkloadActionScope{}
	db := runaidb.DB().WithContext(ctx)

	err := db.Unscoped().
		Table("workloads").
		Select("tenant_id, cluster_id, project_id, department_id").
		Where("tenant_id = ? AND id = ?", tenantID, workloadID).
		Limit(1).
		Find(&scopes)
	if err != nil {
		return nil, api.NewError(http.StatusInternalServerError, "Failed to get workload scope", err)
	}
	if len(scopes) > 0 {
		return &scopes[0], nil
	}

	err = db.Table("workloads_action_history").
		Select("tenant_id, cluster_id, project_id, department_id").
		Where("tenant_id = ? AND workload_id = ?", tenantID, workloadID).
		Order("accepted_at DESC, id DESC").
		Limit(1). // Only the latest saved scope is needed for authorization.
		Find(&scopes)
	if err != nil {
		return nil, api.NewError(http.StatusInternalServerError, "Failed to get retained workload scope", err)
	}
	if len(scopes) == 0 {
		return nil, api.NewError(http.StatusNotFound, "Workload not found", nil)
	}

	return &scopes[0], nil
}

func DbDeleteWorkloadActionHistoryOfTenantId(ctx context.Context, tenantID string) *api.Error {
	_, err := runaidb.DB().WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Delete(&models.WorkloadActionHistory{})
	if err != nil {
		return api.NewError(http.StatusInternalServerError, "Failed to delete tenant workload action history", err)
	}
	return nil
}

func DbDeleteWorkloadActionHistoryOfClusterId(ctx context.Context, tenantID, clusterID string) *api.Error {
	_, err := runaidb.DB().WithContext(ctx).
		Where("tenant_id = ? AND cluster_id = ?", tenantID, clusterID).
		Delete(&models.WorkloadActionHistory{})
	if err != nil {
		return api.NewError(http.StatusInternalServerError, "Failed to delete cluster workload action history", err)
	}
	return nil
}
