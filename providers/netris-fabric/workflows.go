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
	"time"

	"go.temporal.io/sdk/temporal"
	tsdkWorker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// TaskQueue returns the Temporal task queue name for the netris-fabric provider.
func (p *NetrisFabricProvider) TaskQueue() string { return "netris-fabric-tasks" }

// RegisterWorkflows registers all netris-fabric Temporal workflows on the given worker.
func (p *NetrisFabricProvider) RegisterWorkflows(w tsdkWorker.Worker) {
	w.RegisterWorkflow(NetrisFabricSyncWorkflow)
}

// RegisterActivities registers all netris-fabric Temporal activities on the given worker.
func (p *NetrisFabricProvider) RegisterActivities(w tsdkWorker.Worker) {
	activities := &NetrisFabricActivities{provider: p}
	w.RegisterActivity(activities)
}

// NetrisFabricSyncWorkflow is a long-running workflow that listens for NICo
// networking events via signal channels and dispatches the corresponding
// fabric sync activities. It runs indefinitely as a watcher workflow.
func NetrisFabricSyncWorkflow(ctx workflow.Context) error {
	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    2 * time.Minute,
		MaximumAttempts:    5,
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         retryPolicy,
	}

	actCtx := workflow.WithActivityOptions(ctx, activityOptions)

	var activities *NetrisFabricActivities

	for {
		selector := workflow.NewSelector(ctx)

		// Signal: post-create-vpc
		postCreateVPC := workflow.GetSignalChannel(ctx, "post-create-vpc")
		selector.AddReceive(postCreateVPC, func(c workflow.ReceiveChannel, more bool) {
			var payload map[string]interface{}
			c.Receive(ctx, &payload)
			if vpcID, ok := payload["vpc_id"].(string); ok {
				_ = workflow.ExecuteActivity(actCtx, activities.SyncVPC, vpcID).Get(ctx, nil)
			}
		})

		// Signal: post-delete-vpc
		postDeleteVPC := workflow.GetSignalChannel(ctx, "post-delete-vpc")
		selector.AddReceive(postDeleteVPC, func(c workflow.ReceiveChannel, more bool) {
			var payload map[string]interface{}
			c.Receive(ctx, &payload)
			if vpcID, ok := payload["vpc_id"].(string); ok {
				_ = workflow.ExecuteActivity(actCtx, activities.RemoveVPC, vpcID).Get(ctx, nil)
			}
		})

		// Signal: post-create-subnet
		postCreateSubnet := workflow.GetSignalChannel(ctx, "post-create-subnet")
		selector.AddReceive(postCreateSubnet, func(c workflow.ReceiveChannel, more bool) {
			var payload map[string]interface{}
			c.Receive(ctx, &payload)
			subnetID, _ := payload["subnet_id"].(string)
			vpcID, _ := payload["vpc_id"].(string)
			prefix, _ := payload["prefix"].(string)
			if subnetID != "" && vpcID != "" && prefix != "" {
				_ = workflow.ExecuteActivity(actCtx, activities.SyncSubnet, subnetID, vpcID, prefix).Get(ctx, nil)
			}
		})

		// Signal: post-delete-subnet
		postDeleteSubnet := workflow.GetSignalChannel(ctx, "post-delete-subnet")
		selector.AddReceive(postDeleteSubnet, func(c workflow.ReceiveChannel, more bool) {
			var payload map[string]interface{}
			c.Receive(ctx, &payload)
			if subnetID, ok := payload["subnet_id"].(string); ok {
				_ = workflow.ExecuteActivity(actCtx, activities.RemoveSubnet, subnetID).Get(ctx, nil)
			}
		})

		selector.Select(ctx)
	}
}
