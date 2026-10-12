/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package actionhistory_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/internal/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/actionhistory"
	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/database/models"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/runaictx"
	"github.com/google/uuid"
)

func authenticatedContext(tenant, subject, subjectType string) context.Context {
	ctx := context.WithValue(context.Background(), runaictx.CfTenantId, tenant)
	ctx = context.WithValue(ctx, runaictx.CfSubjectID, subject)
	return context.WithValue(ctx, runaictx.CfSubjectType, subjectType)
}

func acceptedWorkload() actionhistory.WorkloadToLog {
	return actionhistory.WorkloadToLog{ID: uuid.New(), ClusterID: uuid.New(), Name: "workload"}
}

func TestAuthenticatedContextValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"nil context", nil},
		{"missing identity", context.Background()},
		{"negative tenant", authenticatedContext("-1", "user@example.test", "user")},
		{"non-numeric tenant", authenticatedContext("not-a-tenant", "user@example.test", "user")},
		{"bigint overflow", authenticatedContext("9223372036854775808", "user@example.test", "user")},
		{"empty subject", authenticatedContext("42", "", "user")},
		{"blank subject", authenticatedContext("42", " \t", "user")},
		{"empty subject type", authenticatedContext("42", "user@example.test", "")},
		{"blank subject type", authenticatedContext("42", "user@example.test", " \t")},
		{"wrong subject type value", context.WithValue(authenticatedContext("42", "user@example.test", "user"), runaictx.CfSubjectType, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := actionhistory.NewWorkloadActionRecord(tc.ctx, api.WORKLOADACTIONTYPE_CREATE, acceptedWorkload(), time.Now()); err == nil {
				t.Fatal("invalid authenticated identity was accepted")
			}
		})
	}
}

func TestAcceptedActionSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, subject, subjectType string
	}{
		{"gateway user", "user@example.test", "User"},
		{"gateway service account", "service-account-id", "ServiceAccount"},
		{"user", "user@example.test", "user"},
		{"service account", "service-account-id", "service-account"},
		{"legacy app", "app-id", "App"},
		{"legacy lowercase app", "app-id", "app"},
		{"future identity type", "identity-id", "FutureIdentity"},
		{"value preserved without trimming", "identity-id", " User "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := authenticatedContext("42", tc.subject, tc.subjectType)
			workload := acceptedWorkload()
			acceptedAt := time.Date(2026, 10, 11, 14, 15, 16, 123456789, time.FixedZone("offset", 3*60*60))
			action := api.WORKLOADACTIONTYPE_CREATE
			first, err := actionhistory.NewWorkloadActionRecord(ctx, action, workload, acceptedAt)
			if err != nil {
				t.Fatal(err)
			}
			if first == nil || first.ID == uuid.Nil || first.ID.Version() != 4 {
				t.Fatal("accepted command did not get a UUID")
			}
			if first.TenantID != 42 || first.Subject != tc.subject || first.SubjectType != tc.subjectType {
				t.Fatalf("record identity differs from authenticated context: %+v", first)
			}
			if first.WorkloadID != workload.ID || first.ClusterID != workload.ClusterID || first.WorkloadName != workload.Name || first.Action != string(api.WORKLOADACTIONTYPE_CREATE) {
				t.Fatalf("incorrect accepted workload snapshot: %+v", first)
			}
			if !first.AcceptedAt.Equal(acceptedAt.Truncate(time.Microsecond)) || first.AcceptedAt.Location() != time.UTC {
				t.Fatalf("acceptance timestamp changed: %v", first.AcceptedAt)
			}
			// Mutating caller data and replacing context cannot alter a retained record.
			workload.Name = "changed"
			ctx = context.WithValue(ctx, runaictx.CfSubjectID, "another-user@example.test")
			second, err := actionhistory.NewWorkloadActionRecord(ctx, action, workload, acceptedAt)
			if err != nil {
				t.Fatal(err)
			}
			if first.ID == second.ID {
				t.Fatal("separately accepted API retry reused a record ID")
			}
			if first.Subject != tc.subject || first.WorkloadName != "workload" {
				t.Fatal("later caller changes modified the original snapshot")
			}
			if second.Subject != "another-user@example.test" {
				t.Fatal("new command reused the previous actor")
			}
		})
	}
}

func TestAuthenticatedTenantBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  uint64
	}{
		{"0", 0},
		{"9223372036854775807", 9223372036854775807},
	} {
		ctx := authenticatedContext(tc.input, "user@example.test", "user")
		record, err := actionhistory.NewWorkloadActionRecord(ctx, api.WORKLOADACTIONTYPE_CREATE, acceptedWorkload(), time.Now())
		if err != nil || record == nil || uint64(record.TenantID) != tc.want {
			t.Fatalf("tenant %q produced %+v, %v", tc.input, record, err)
		}
	}
}

func TestConcurrentAcceptedBatchItems(t *testing.T) {
	ctx := authenticatedContext("42", "user@example.test", "user")
	const count = 32
	records := make(chan *models.WorkloadActionHistory, count)
	errors := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, err := actionhistory.NewWorkloadActionRecord(ctx, api.WORKLOADACTIONTYPE_DELETE, acceptedWorkload(), time.Now())
			if err != nil {
				errors <- err
				return
			}
			records <- record
		}()
	}
	wg.Wait()
	close(records)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	ids := make(map[uuid.UUID]struct{}, count)
	for record := range records {
		if record == nil || record.Subject != "user@example.test" || record.TenantID != 42 {
			t.Fatalf("incorrect concurrent record: %+v", record)
		}
		if _, duplicate := ids[record.ID]; duplicate {
			t.Fatal("different accepted items reused an action UUID")
		}
		ids[record.ID] = struct{}{}
	}
	if len(ids) != count {
		t.Fatalf("got %d records for %d accepted items", len(ids), count)
	}
}
