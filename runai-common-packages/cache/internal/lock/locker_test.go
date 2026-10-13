/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package lock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dsx-ai-factory/run-ai/runai-common-packages/cache/v2/internal/observability"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/cache/v2/mocks"
	"github.com/dsx-ai-factory/run-ai/runai-common-packages/cache/v2/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func initLockerTest(t *testing.T, shared bool) (Locker, *mocks.CacheClienter) {
	observabilityHandler := observability.NewDummyObservabilityHandler()
	cacheClientMock := mocks.NewCacheClienter(t)
	serviceLockStore := options.Store{
		Name:    "service-lock-store",
		Options: options.NewStoreOptions(options.MaxRecordSizeKB(1)),
	}
	sharedLockStore := options.Store{
		Name:    "shared-lock-store",
		Options: options.NewStoreOptions(options.MaxRecordSizeKB(1)),
	}
	var locker Locker
	if shared {
		locker = newLocker(cacheClientMock, observabilityHandler, sharedLockStore, "unique-id", "key")
	} else {
		locker = newLocker(cacheClientMock, observabilityHandler, serviceLockStore, "unique-id", "key")
	}

	return locker, cacheClientMock
}
func TestLock(t *testing.T) {
	ctx := context.Background()
	ttl := time.Second
	locker, cacheClientMock := initLockerTest(t, false)
	cacheClientMock.On("SetIfAbsent", context.WithoutCancel(ctx), "service-lock-store", "key", mock.Anything, &ttl).Return(true, nil)

	err := locker.Lock(ctx, ttl)
	assert.NoError(t, err)
}

func TestSharedLock(t *testing.T) {
	ctx := context.Background()
	ttl := time.Second
	locker, cacheClientMock := initLockerTest(t, true)
	cacheClientMock.On("SetIfAbsent", context.WithoutCancel(ctx), "shared-lock-store", "key", mock.Anything, &ttl).Return(true, nil)
	err := locker.Lock(ctx, ttl)
	assert.NoError(t, err)
}

func TestLock_Failed(t *testing.T) {
	ctx := context.Background()
	ttl := time.Second
	locker, cacheClientMock := initLockerTest(t, false)
	cacheClientMock.On("SetIfAbsent", context.WithoutCancel(ctx), "service-lock-store", "key", mock.Anything, &ttl).Return(false, errors.New("failed"))
	err := locker.Lock(ctx, ttl)
	assert.EqualError(t, err, "failed to lock key service-lock-store.key: failed")
}

func TestLock_AlreadyLocked(t *testing.T) {
	ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
	ttl := time.Second
	locker, cacheClientMock := initLockerTest(t, false)
	cacheClientMock.On("SetIfAbsent", context.WithoutCancel(ctx), "service-lock-store", "key", mock.Anything, &ttl).Return(false, nil)
	err := locker.Lock(ctx, ttl)
	assert.EqualError(t, err, "key service-lock-store.key already locked")
	c()
}

// A client may store the lock and still return an error, for example when it
// failed to apply the TTL. That must not be reported as an acquired lock: the
// caller would never unlock a lock that has no expiry.
func TestLock_FailedAfterAcquiringReleasesTheKey(t *testing.T) {
	ctx := context.Background()
	ttl := time.Second
	locker, cacheClientMock := initLockerTest(t, false)
	cacheClientMock.On("SetIfAbsent", context.WithoutCancel(ctx), "service-lock-store", "key", mock.Anything, &ttl).
		Return(true, errors.New("failed to set expiration"))
	cacheClientMock.On("Delete", context.WithoutCancel(ctx), "service-lock-store", "key").Return(nil)

	err := locker.Lock(ctx, ttl)

	assert.EqualError(t, err, "failed to lock key service-lock-store.key: failed to set expiration")
	cacheClientMock.AssertCalled(t, "Delete", context.WithoutCancel(ctx), "service-lock-store", "key")
}

func TestLock_FailedAfterAcquiringReportsAnUnreleasedKey(t *testing.T) {
	ctx := context.Background()
	ttl := time.Second
	locker, cacheClientMock := initLockerTest(t, false)
	cacheClientMock.On("SetIfAbsent", context.WithoutCancel(ctx), "service-lock-store", "key", mock.Anything, &ttl).
		Return(true, errors.New("failed to set expiration"))
	cacheClientMock.On("Delete", context.WithoutCancel(ctx), "service-lock-store", "key").Return(errors.New("delete failed"))

	err := locker.Lock(ctx, ttl)

	assert.EqualError(t, err, "failed to lock key service-lock-store.key: failed to set expiration (and failed to release it: delete failed)")
}

// A client that did not store the lock and returned an error has nothing to
// release.
func TestLock_FailedWithoutAcquiringDoesNotRelease(t *testing.T) {
	ctx := context.Background()
	ttl := time.Second
	locker, cacheClientMock := initLockerTest(t, false)
	cacheClientMock.On("SetIfAbsent", context.WithoutCancel(ctx), "service-lock-store", "key", mock.Anything, &ttl).
		Return(false, errors.New("store is full"))

	err := locker.Lock(ctx, ttl)

	assert.EqualError(t, err, "failed to lock key service-lock-store.key: store is full")
	cacheClientMock.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything, mock.Anything)
}
