/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package actionhistory_test

import (
	"testing"

	"github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/actionhistory"
)

func TestActionValidation(t *testing.T) {
	for _, action := range []actionhistory.Action{actionhistory.Create, actionhistory.Update, actionhistory.Delete, actionhistory.Suspend, actionhistory.Resume} {
		t.Run(string(action), func(t *testing.T) {
			if !action.Valid() {
				t.Fatalf("valid action %q rejected", action)
			}
		})
	}
	invalidActions := []struct {
		name   string
		action actionhistory.Action
	}{
		{name: "empty", action: ""},
		{name: "patch", action: "patch"},
		{name: "running", action: "running"},
		{name: "restart", action: "restart"},
		{name: "wrong_case", action: "Create"},
	}

	for _, tc := range invalidActions {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if tc.action.Valid() {
				t.Fatalf("Valid() returned true for unsupported action %q", tc.action)
			}
		})
	}
}
