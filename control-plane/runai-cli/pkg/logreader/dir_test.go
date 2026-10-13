/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package logreader

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeLogFile(t *testing.T, dir, name string, lines int, modTime time.Time) {
	t.Helper()

	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "%s-%d\n", name, i)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func getLogList(t *testing.T, dir string, tail int) []string {
	t.Helper()

	reader, err := NewLogReader(dir, tail)
	if err != nil {
		t.Fatalf("new log reader: %v", err)
	}
	lines, err := reader.GetLogList()
	if err != nil {
		t.Fatalf("get log list: %v", err)
	}
	return lines
}

func TestGetLogListReturnsExactlyTailLines(t *testing.T) {
	for _, tail := range []int{0, 1, 5, 10} {
		t.Run(fmt.Sprintf("tail=%d", tail), func(t *testing.T) {
			dir := t.TempDir()
			writeLogFile(t, dir, "metrics.log", 10, time.Now())

			if got := getLogList(t, dir, tail); len(got) != tail {
				t.Fatalf("got %d lines, want %d: %v", len(got), tail, got)
			}
		})
	}
}

func TestGetLogListReturnsEveryLineWhenTailExceedsThem(t *testing.T) {
	dir := t.TempDir()
	writeLogFile(t, dir, "metrics.log", 10, time.Now())

	if got := getLogList(t, dir, 11); len(got) != 10 {
		t.Fatalf("got %d lines, want 10: %v", len(got), got)
	}
}

// The budget covers the whole list, not each file: a tail of 4 over two files
// of 3 lines must return 4 lines, not one extra per file.
func TestGetLogListSpansFilesWithoutExceedingTail(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeLogFile(t, dir, "old.log", 3, now.Add(-time.Hour))
	writeLogFile(t, dir, "new.log", 3, now)

	want := []string{"new.log-1", "new.log-2", "new.log-3", "old.log-1"}
	if got := getLogList(t, dir, 4); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v (the newest file is read first and the budget spans the remaining files)", got, want)
	}
}
