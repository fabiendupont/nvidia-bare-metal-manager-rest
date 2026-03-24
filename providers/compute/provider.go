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

package compute

import (
	"context"

	"github.com/NVIDIA/ncx-infra-controller-rest/db/pkg/db"
	"github.com/NVIDIA/ncx-infra-controller-rest/provider"
)

// ComputeProvider implements the compute feature provider.
type ComputeProvider struct {
	service *SQLService
}

// New creates a new ComputeProvider.
func New() *ComputeProvider {
	return &ComputeProvider{}
}

func (p *ComputeProvider) Name() string         { return "nico-compute" }
func (p *ComputeProvider) Version() string      { return "1.0.6" }
func (p *ComputeProvider) Features() []string   { return []string{"compute"} }
func (p *ComputeProvider) Dependencies() []string { return []string{"nico-networking"} }

func (p *ComputeProvider) Init(ctx provider.ProviderContext) error {
	p.service = NewSQLService(ctx.DB)
	return nil
}

func (p *ComputeProvider) Shutdown(_ context.Context) error {
	return nil
}

// Service returns the compute service for cross-domain access.
func (p *ComputeProvider) Service() Service {
	return p.service
}

// NewService creates a compute Service from a DB session. This is used
// during the migration period when providers aren't fully wired up yet.
func NewService(dbSession *db.Session) Service {
	return NewSQLService(dbSession)
}
