/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

// Editors still write the deprecated names. Keep one value in their models so an
// edit or deletion cannot resubmit a stale alias from a dual-field API response.
export function legacyComputeForEditor<T>(source: T): T {
  if (!source || typeof source !== "object") return source;
  const result = { ...source } as Record<string, unknown>;
  if (!("gpuPortionRequest" in result) && "gpuMemoryPortionRequest" in result) {
    result.gpuPortionRequest = result.gpuMemoryPortionRequest;
  }
  if (!("gpuPortionLimit" in result) && "gpuMemoryPortionLimit" in result) {
    result.gpuPortionLimit = result.gpuMemoryPortionLimit;
  }
  delete result.gpuMemoryPortionRequest;
  delete result.gpuMemoryPortionLimit;
  return result as T;
}

export function legacyPolicyForEditor<T>(source: T): T {
  if (!source || typeof source !== "object" || Array.isArray(source)) return source;
  const result = { ...source } as Record<string, unknown>;
  if ("policy" in result) result.policy = legacyPolicyForEditor(result.policy);
  if ("effective" in result) result.effective = legacyPolicyForEditor(result.effective);
  if ("defaults" in result) result.defaults = legacyPolicyForEditor(result.defaults);
  if ("rules" in result) result.rules = legacyPolicyForEditor(result.rules);
  if ("worker" in result) result.worker = legacyPolicyForEditor(result.worker);
  if ("master" in result) result.master = legacyPolicyForEditor(result.master);
  if ("leader" in result) result.leader = legacyPolicyForEditor(result.leader);
  if ("compute" in result) result.compute = legacyComputeForEditor(result.compute);
  return result as T;
}
