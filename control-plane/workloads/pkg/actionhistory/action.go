/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package actionhistory

type Action string

const (
	Create  Action = "create"
	Update  Action = "update"
	Delete  Action = "delete"
	Suspend Action = "suspend"
	Resume  Action = "resume"
)

func (a Action) Valid() bool {
	switch a {
	case Create, Update, Delete, Suspend, Resume:
		return true
	default:
		return false
	}
}
