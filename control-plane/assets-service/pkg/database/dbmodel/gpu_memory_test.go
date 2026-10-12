/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package dbmodel

import (
	"github.com/dsx-ai-factory/run-ai/control-plane/assets-service/generated/pkg/api"
	"gorm.io/gorm/schema"
	"sync"
	"testing"
)

func TestGPUMemoryAliasesHaveNoDatabaseColumns(t *testing.T) {
	for _, model := range []any{&ComputeAssetRec{}, &ComputeAssetUpdate{}, &ComputeFullRec{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"GpuMemoryPortionRequest", "GpuMemoryPortionLimit"} {
			field := parsed.FieldsByName[name]
			if field == nil || field.DBName != "" || field.Creatable || field.Updatable {
				t.Fatalf("%T persists public alias %s: %#v", model, name, field)
			}
		}
		for _, name := range []string{"gpu_portion_request", "gpu_portion_limit"} {
			if parsed.FieldsByDBName[name] == nil {
				t.Fatalf("lost stored column %s", name)
			}
		}
	}
}
func TestGPUMemoryAssetRoundTrip(t *testing.T) {
	request, limit := .25, .5
	input := api.ComputeAssetSpec{GpuMemoryPortionRequest: &request, GpuMemoryPortionLimit: &limit}
	stored, err := StoredComputeAsset(input)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GpuMemoryPortionRequest != nil || stored.GpuMemoryPortionLimit != nil {
		t.Fatal("persisted new alias")
	}
	response := LegacyCompatibleComputeAsset(stored)
	if *response.GpuPortionRequest != request || *response.GpuMemoryPortionRequest != request || *response.GpuMemoryPortionLimit != limit {
		t.Fatal("response lost aliases")
	}
	*response.GpuPortionRequest = .9
	if *stored.GpuPortionRequest != .25 || *response.GpuMemoryPortionRequest != .25 {
		t.Fatal("shared alias pointers")
	}
}

func TestLegacyWholeGPUAssetResponse(t *testing.T) {
	value := 4.0
	stored := api.ComputeAssetSpec{GpuPortionRequest: &value}
	response := LegacyCompatibleComputeAsset(stored)
	if response.GpuMemoryPortionRequest != nil || response.GpuPortionRequest == nil || *response.GpuPortionRequest != value || response.GpuDevicesRequest != nil {
		t.Fatal("legacy record changed on read")
	}
}
