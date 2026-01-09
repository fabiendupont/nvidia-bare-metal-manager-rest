// SPDX-FileCopyrightText: Copyright (c) 2021-2023 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: LicenseRef-NvidiaProprietary
//
// NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
// property and proprietary rights in and to this material, related
// documentation and any modifications thereto. Any use, reproduction,
// disclosure or distribution of this material and related documentation
// without an express license agreement from NVIDIA CORPORATION or
// its affiliates is strictly prohibited.

package health

import (
	"time"

	wflows "github.com/nvidia/carbide-rest/workflow-schema/schema/site-agent/workflows/v1"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

// GetHealthActivity - get the health status
func (ac *HealthWorkflow) GetHealthActivity() (*wflows.HealthStatus, error) {
	status := &wflows.HealthStatus{
		Timestamp: timestamppb.New(time.Now()),
		SiteInventoryCollection: &wflows.HealthStatusMsg{
			State: ManagerAccess.Data.EB.Managers.Health.Inventory.State,
		},
		SiteControllerConnection: &wflows.HealthStatusMsg{
			State: ManagerAccess.Data.EB.Managers.Health.CarbideInterface.State,
		},
		SiteAgentHighAvailability: &wflows.HealthStatusMsg{
			State: ManagerAccess.Data.EB.Managers.Health.Availabilty.State,
		},
	}

	return status, nil
}
