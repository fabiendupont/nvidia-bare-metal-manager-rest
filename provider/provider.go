/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package provider

import (
	"context"
	"io/fs"

	echo "github.com/labstack/echo/v4"
	tsdkWorker "go.temporal.io/sdk/worker"
)

// Provider is the contract every NICo provider implements.
type Provider interface {
	Name() string
	Version() string
	Features() []string
	Dependencies() []string
	Init(ctx ProviderContext) error
	Shutdown(ctx context.Context) error
}

// APIProvider adds REST routes to the Echo server.
type APIProvider interface {
	Provider
	RegisterRoutes(group *echo.Group)
}

// WorkflowProvider adds Temporal workflows and activities.
type WorkflowProvider interface {
	Provider
	TaskQueue() string
	RegisterWorkflows(w tsdkWorker.Worker)
	RegisterActivities(w tsdkWorker.Worker)
}

// MigrationProvider manages its own DB tables.
type MigrationProvider interface {
	Provider
	MigrationSource() fs.FS
}
