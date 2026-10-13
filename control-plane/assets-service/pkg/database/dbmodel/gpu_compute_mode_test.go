/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package dbmodel

import (
	"sync"
	"testing"

	"github.com/dsx-ai-factory/run-ai/control-plane/assets-service/generated/pkg/api"
	"github.com/dsx-ai-factory/run-ai/runai-application-packages/convutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestComputeModeStorage(t *testing.T) {
	for _, model := range []interface{}{&ComputeAssetRec{}, &ComputeAssetUpdate{}, &ComputeFullRec{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		require.NoError(t, err)
		field := parsed.FieldsByDBName["gpu_compute_mode"]
		require.NotNil(t, field)
		require.True(t, field.Creatable)
		require.True(t, field.Updatable)
		require.NotContains(t, parsed.FieldsByDBName, "compute_mode")
	}
	source := api.ComputeAssetSpec{GpuComputeMode: convutils.Ref(api.GPUCOMPUTEMODE_SM_SHARING)}
	stored, err := StoredComputeAsset(source)
	require.NoError(t, err)
	response := LegacyCompatibleComputeAsset(stored)
	require.Equal(t, api.GPUCOMPUTEMODE_SM_SHARING, *response.GpuComputeMode)
}
