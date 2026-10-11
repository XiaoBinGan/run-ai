/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services_crud

import (
	"context"
	"time"

	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/actionhistory"
	workloadsApi "github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/client/generated"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/dao"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/models"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/convutils"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/logging"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/runaictx"
	"github.com/google/uuid"
)

// Log history write failures without failing the accepted workload action.
func recordWorkloadAction(ctx context.Context, workload actionhistory.WorkloadDetails, action actionhistory.Action, acceptedAt time.Time) {
	record, err := actionhistory.WorkloadActionRecord(ctx, action, workload, acceptedAt)
	if err == nil {
		err = dao.DbInsertWorkloadActions(ctx, []models.WorkloadActionHistory{*record})
	}
	if err != nil {
		logging.WithCtx(runaictx.LoggerHook(ctx)).Errorf("Failed to record accepted workload %s action for %s: %v", action, workload.ID, err)
	}
}

func recordWorkloadStateAction(ctx context.Context, workload *models.WorkloadView, desiredPhase api.WorkloadDesiredPhase, acceptedAt time.Time) {
	var action actionhistory.Action
	switch desiredPhase {
	case api.WORKLOADDESIREDPHASE_DELETED:
		action = actionhistory.Delete
	case api.WORKLOADDESIREDPHASE_STOPPED:
		if !supportsV1RunStateHistory(workload.Type) {
			return
		}
		action = actionhistory.Suspend
	case api.WORKLOADDESIREDPHASE_RUNNING:
		if !supportsV1RunStateHistory(workload.Type) {
			return
		}
		action = actionhistory.Resume
	default:
		return
	}
	recordWorkloadAction(ctx, actionhistory.WorkloadDetails{ID: workload.ID, ClusterID: workload.ClusterID, Name: workload.Name, ProjectID: workload.ProjectID, DepartmentID: workload.DepartmentID}, action, acceptedAt)
}

func supportsV1RunStateHistory(workloadType string) bool {
	return workloadType == Workspace || workloadType == Training || workloadType == Distributed
}

func recordWorkloadResponseAction(ctx context.Context, response workloadsApi.SupersetWorkload, action actionhistory.Action, acceptedAt time.Time) {
	workloadID, err := uuid.Parse(response.WorkloadId)
	if err != nil {
		logging.WithCtx(runaictx.LoggerHook(ctx)).Errorf("Failed to record accepted workload %s: invalid workload ID: %v", action, err)
		return
	}
	clusterID, err := uuid.Parse(response.ClusterId)
	if err != nil {
		logging.WithCtx(runaictx.LoggerHook(ctx)).Errorf("Failed to record accepted workload %s for %s: invalid cluster ID: %v", action, workloadID, err)
		return
	}
	recordWorkloadAction(ctx, actionhistory.WorkloadDetails{
		ID: workloadID, ClusterID: clusterID, Name: response.Name,
		ProjectID: response.ProjectId, DepartmentID: convutils.Deref(response.DepartmentId, ""),
	}, action, acceptedAt)
}

func recordWorkloadCreate(ctx context.Context, params *createWorkloadParams) {
	recordWorkloadResponseAction(ctx, params.Response, actionhistory.Create, params.AcceptedAt)
}
