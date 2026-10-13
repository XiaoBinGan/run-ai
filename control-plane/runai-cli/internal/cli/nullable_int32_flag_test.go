/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package cli_test

import (
	"github.com/dsx-ai-factory/run-ai/control-plane/runai-cli/internal/cli"
	openapi "github.com/dsx-ai-factory/run-ai/control-plane/workloads/pkg/client/generated"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NullableInt32", func() {
	var (
		ni *cli.NullableInt32Flag
	)

	BeforeEach(func() {
		ni = cli.NewNullableInt32Flag(&openapi.NullableInt32{})
	})

	Describe("Set", func() {
		It("should set a valid integer value", func() {
			err := ni.Set("42")
			Expect(err).NotTo(HaveOccurred())
			Expect(ni.IsSet()).To(BeTrue())
			Expect(*ni.Value()).To(Equal(int32(42)))
		})

		It("should fail on invalid input", func() {
			err := ni.Set("invalid")
			Expect(err).To(HaveOccurred())
			Expect(ni.IsSet()).To(BeFalse())
		})

		It("should fail on a value that does not fit in an int32", func() {
			for _, value := range []string{"2147483648", "4294967295", "-2147483649"} {
				ni = cli.NewNullableInt32Flag(&openapi.NullableInt32{})
				err := ni.Set(value)
				Expect(err).To(HaveOccurred(), "value %q must be rejected", value)
				Expect(ni.IsSet()).To(BeFalse(), "value %q must leave the flag unset", value)
			}
		})

		It("should set the int32 boundary values", func() {
			for _, tc := range []struct {
				value string
				want  int32
			}{
				{value: "2147483647", want: 2147483647},
				{value: "-2147483648", want: -2147483648},
			} {
				ni = cli.NewNullableInt32Flag(&openapi.NullableInt32{})
				Expect(ni.Set(tc.value)).To(Succeed())
				Expect(*ni.Value()).To(Equal(tc.want))
			}
		})
	})

	Describe("String", func() {
		It("should return an empty string when not set", func() {
			Expect(ni.String()).To(Equal(""))
		})

		It("should return the integer value as a string when set", func() {
			err := ni.Set("123")
			if err != nil {
				Fail("Failed to set value")
			}
			Expect(ni.String()).To(Equal("123"))
		})
	})

	Describe("IsSet", func() {
		It("should return false when value is not set", func() {
			Expect(ni.IsSet()).To(BeFalse())
		})

		It("should return true when value is set", func() {
			err := ni.Set("99")
			if err != nil {
				Fail("Failed to set value")
			}
			Expect(ni.IsSet()).To(BeTrue())
		})
	})
})
