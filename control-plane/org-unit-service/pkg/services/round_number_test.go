/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestRoundNumber(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    *float64
		expected float64
	}{
		{name: "nil is zero", value: nil, expected: 0},
		{name: "zero stays zero", value: ptr.To(0.0), expected: 0},
		{name: "three decimals are kept", value: ptr.To(4.567), expected: 4.567},
		{name: "positive zero from a negative value", value: ptr.To(-0.0), expected: 0},
		{name: "product just below the integer is rounded up", value: ptr.To(1.001), expected: 1.001},
		{name: "another product just below the integer", value: ptr.To(1.005), expected: 1.005},
		{name: "fourth decimal is dropped", value: ptr.To(1.0006), expected: 1.001},
		{name: "negative product just below the integer", value: ptr.To(-1.001), expected: -1.001},
		{name: "big value keeps its three decimals", value: ptr.To(100.0009), expected: 100.001},
		{name: "large value does not overflow", value: ptr.To(1234567890123.456), expected: 1234567890123.456},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, roundNumber(tc.value))
		})
	}
}
