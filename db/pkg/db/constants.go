/*
 * SPDX-FileCopyrightText: Copyright (c) 2021-2023 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: LicenseRef-NvidiaProprietary
 *
 * NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
 * property and proprietary rights in and to this material, related
 * documentation and any modifications thereto. Any use, reproduction,
 * disclosure or distribution of this material and related documentation
 * without an express license agreement from NVIDIA CORPORATION or
 * its affiliates is strictly prohibited.
 */

package db

const (
	// DefaultPageSize is the size for query results to request from DB
	DefaultPageSize = 20

	// MaxBatchItems limits the maximum number of items allowed in a single batch operation
	// to prevent performance degradation and potential timeouts from overly large batches.
	MaxBatchItems = 100

	// MaxBatchItemsToTrace limits the number of items traced in detail for batch operations
	// to avoid producing overly-large spans and reduce the risk of hitting tracing backend limits.
	// Items beyond this limit will still be processed but won't have their individual field values traced.
	MaxBatchItemsToTrace = 20
)
