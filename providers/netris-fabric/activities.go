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

package netrisfabric

import (
	"context"
)

// NetrisFabricActivities contains the Temporal activities for fabric sync.
type NetrisFabricActivities struct {
	provider *NetrisFabricProvider
}

// SyncVPC creates a Netris VPC matching the NICo VPC.
func (a *NetrisFabricActivities) SyncVPC(ctx context.Context, vpcID string) error {
	return a.provider.SyncVPCToFabric(ctx, vpcID)
}

// RemoveVPC deletes the Netris VPC corresponding to the NICo VPC.
func (a *NetrisFabricActivities) RemoveVPC(ctx context.Context, vpcID string) error {
	return a.provider.RemoveVPCFromFabric(ctx, vpcID)
}

// SyncSubnet creates a Netris VNET matching the NICo subnet.
func (a *NetrisFabricActivities) SyncSubnet(ctx context.Context, subnetID string, vpcID string, prefix string) error {
	return a.provider.SyncSubnetToFabric(ctx, subnetID, vpcID, prefix)
}

// RemoveSubnet deletes the Netris VNET corresponding to the NICo subnet.
func (a *NetrisFabricActivities) RemoveSubnet(ctx context.Context, subnetID string) error {
	return a.provider.RemoveSubnetFromFabric(ctx, subnetID)
}
