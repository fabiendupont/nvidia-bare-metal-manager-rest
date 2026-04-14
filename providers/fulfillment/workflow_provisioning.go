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

package fulfillment

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	cdbm "github.com/NVIDIA/ncx-infra-controller-rest/db/pkg/db/model"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// TenantProvisioningWorkflow orchestrates the full provisioning sequence
// when a tenant places an order. Each step is a Temporal activity that
// calls the appropriate NICo service interface.
func TenantProvisioningWorkflow(ctx workflow.Context, orderID uuid.UUID) error {
	logger := log.With().Str("Workflow", "TenantProvisioning").
		Str("OrderID", orderID.String()).Logger()

	logger.Info().Msg("starting workflow")

	retrypolicy := &temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    2 * time.Minute,
		MaximumAttempts:    15,
	}
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         retrypolicy,
	}

	ctx = workflow.WithActivityOptions(ctx, options)

	var activities FulfillmentActivities
	var provisioning ProvisioningActivities

	// Step 1: Validate order and template
	logger.Info().Msg("validating order")
	var order Order
	err := workflow.ExecuteActivity(ctx, activities.ValidateOrder, orderID).Get(ctx, &order)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to validate order")
		return err
	}

	// Step 2: Update order status to Provisioning
	logger.Info().Msg("updating order status to provisioning")
	err = workflow.ExecuteActivity(ctx, activities.UpdateOrderStatus, orderID, OrderStatusProvisioning, "provisioning started").Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to update order status")
		return err
	}

	// Step 3: Create service record
	logger.Info().Msg("creating service record")
	var service Service
	err = workflow.ExecuteActivity(ctx, activities.CreateService, &order).Get(ctx, &service)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to create service record")
		return err
	}

	// Step 4: Create VPC via networking service
	logger.Info().Msg("provisioning VPC")
	var vpc cdbm.Vpc
	// TODO: Site ID should come from order parameters or allocation constraints, not tenant ID.
	// This is a known placeholder — tenant and site are different concepts.
	siteID := order.TenantID
	err = workflow.ExecuteActivity(ctx, provisioning.ProvisionVPC, service.ID, siteID).Get(ctx, &vpc)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to provision VPC")
		_ = workflow.ExecuteActivity(ctx, activities.UpdateOrderStatus, orderID, OrderStatusFailed, "VPC provisioning failed").Get(ctx, nil)
		return err
	}

	// Step 5: Allocate compute via compute service
	logger.Info().Msg("provisioning compute")
	err = workflow.ExecuteActivity(ctx, provisioning.ProvisionCompute, service.ID, siteID).Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to provision compute")
		_ = workflow.ExecuteActivity(ctx, activities.UpdateOrderStatus, orderID, OrderStatusFailed, "compute provisioning failed").Get(ctx, nil)
		return err
	}

	// Step 6: Mark service as Active, order as Ready
	logger.Info().Msg("marking service as active")
	err = workflow.ExecuteActivity(ctx, activities.MarkServiceActive, service.ID).Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to mark service as active")
		return err
	}

	logger.Info().Msg("updating order status to ready")
	err = workflow.ExecuteActivity(ctx, activities.UpdateOrderStatus, orderID, OrderStatusReady, "provisioning complete").Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to update order status to ready")
		return err
	}

	logger.Info().Msg("completing workflow")
	return nil
}

// TenantTeardownWorkflow reverses provisioning by tearing down resources
// in reverse order via service interfaces, then marks the service as terminated.
func TenantTeardownWorkflow(ctx workflow.Context, serviceID uuid.UUID) error {
	logger := log.With().Str("Workflow", "TenantTeardown").
		Str("ServiceID", serviceID.String()).Logger()

	logger.Info().Msg("starting workflow")

	retrypolicy := &temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    2 * time.Minute,
		MaximumAttempts:    15,
	}
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         retrypolicy,
	}

	ctx = workflow.WithActivityOptions(ctx, options)

	var activities FulfillmentActivities
	var provisioning ProvisioningActivities

	var teardownErrors []string

	// Step 1: Tear down compute resources
	logger.Info().Msg("tearing down compute resources")
	if err := workflow.ExecuteActivity(ctx, provisioning.TeardownCompute, serviceID).Get(ctx, nil); err != nil {
		logger.Warn().Err(err).Msg("failed to tear down compute")
		teardownErrors = append(teardownErrors, "compute: "+err.Error())
	}

	// Step 2: Tear down VPC
	logger.Info().Msg("tearing down VPC")
	if err := workflow.ExecuteActivity(ctx, provisioning.TeardownVPC, serviceID).Get(ctx, nil); err != nil {
		logger.Warn().Err(err).Msg("failed to tear down VPC")
		teardownErrors = append(teardownErrors, "vpc: "+err.Error())
	}

	// Step 3: Mark service as terminated
	logger.Info().Msg("marking service as terminated")
	if err := workflow.ExecuteActivity(ctx, activities.MarkServiceTerminated, serviceID).Get(ctx, nil); err != nil {
		logger.Warn().Err(err).Msg("failed to mark service as terminated")
		return err
	}

	logger.Info().Msg("completing workflow")
	if len(teardownErrors) > 0 {
		return fmt.Errorf("teardown completed with errors: %s", strings.Join(teardownErrors, "; "))
	}
	return nil
}

// ServiceScaleWorkflow modifies a running service by adjusting its
// compute resources via the compute service interface.
func ServiceScaleWorkflow(ctx workflow.Context, serviceID uuid.UUID, params map[string]interface{}) error {
	logger := log.With().Str("Workflow", "ServiceScale").
		Str("ServiceID", serviceID.String()).Logger()

	logger.Info().Msg("starting workflow")

	retrypolicy := &temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    2 * time.Minute,
		MaximumAttempts:    15,
	}
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         retrypolicy,
	}

	ctx = workflow.WithActivityOptions(ctx, options)

	var provisioning ProvisioningActivities

	// Scale compute resources based on parameters
	logger.Info().Interface("params", params).Msg("scaling compute resources")
	err := workflow.ExecuteActivity(ctx, provisioning.ScaleCompute, serviceID, params).Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to scale compute")
		return err
	}

	logger.Info().Msg("completing workflow")
	return nil
}
