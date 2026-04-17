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

package health

import (
	"time"

	"github.com/rs/zerolog/log"

	"go.temporal.io/sdk/temporal"
	tsdkWorker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// TaskQueue returns the Temporal task queue name for the health provider.
func (p *HealthProvider) TaskQueue() string { return "health-tasks" }

// RegisterWorkflows registers all health-domain Temporal workflows on the given worker.
func (p *HealthProvider) RegisterWorkflows(w tsdkWorker.Worker) {
	w.RegisterWorkflow(FaultRemediationWorkflow)
	w.RegisterWorkflow(FaultRetentionWorkflow)
}

// RegisterActivities registers all health-domain Temporal activities on the given worker.
func (p *HealthProvider) RegisterActivities(w tsdkWorker.Worker) {
	activities := &HealthActivities{
		faultStore:          p.faultStore,
		serviceEventStore:   p.serviceEventStore,
		classificationStore: p.classificationStore,
	}
	w.RegisterActivity(activities)
}

// FaultRemediationWorkflow orchestrates the full fault remediation lifecycle.
// Steps: ClassifyAndRoute -> IsolateFault -> RemediateGPU -> ValidateRecovery -> RestoreService.
// If any step fails after retries, the fault is escalated.
func FaultRemediationWorkflow(ctx workflow.Context, faultEventID string) error {
	logger := log.With().Str("Workflow", "FaultRemediation").
		Str("FaultEventID", faultEventID).Logger()

	logger.Info().Msg("starting workflow")

	retrypolicy := &temporal.RetryPolicy{
		InitialInterval:    2 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    2 * time.Minute,
		MaximumAttempts:    15,
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         retrypolicy,
	}

	actCtx := workflow.WithActivityOptions(ctx, activityOptions)

	var activities HealthActivities

	// Step 1: ClassifyAndRoute — look up fault, determine remediation strategy
	logger.Info().Msg("classifying and routing fault")
	var mapping ClassificationMapping
	err := workflow.ExecuteActivity(actCtx, activities.ClassifyAndRoute, faultEventID).Get(ctx, &mapping)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to classify and route fault, escalating")
		_ = workflow.ExecuteActivity(actCtx, activities.EscalateFault, faultEventID, "classification failed: "+err.Error()).Get(ctx, nil)
		return err
	}

	// Step 2: IsolateFault — set machine maintenance mode, create service_event
	logger.Info().Msg("isolating fault")
	err = workflow.ExecuteActivity(actCtx, activities.IsolateFault, faultEventID).Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to isolate fault, escalating")
		_ = workflow.ExecuteActivity(actCtx, activities.EscalateFault, faultEventID, "isolation failed: "+err.Error()).Get(ctx, nil)
		return err
	}

	// Step 3: Remediate — execute component-specific recovery action
	logger.Info().Msg("remediating fault")
	var remediateErr error
	switch mapping.Component {
	case ComponentGPU:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediateGPU, faultEventID, mapping).Get(ctx, nil)
	case ComponentNVSwitch:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediateNVSwitch, faultEventID, mapping).Get(ctx, nil)
	case ComponentPower:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediatePower, faultEventID, mapping).Get(ctx, nil)
	case ComponentNetwork:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediateNetwork, faultEventID, mapping).Get(ctx, nil)
	case ComponentBMC:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediateBMC, faultEventID, mapping).Get(ctx, nil)
	case ComponentStorage:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediateStorage, faultEventID, mapping).Get(ctx, nil)
	case ComponentCooling:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediateCooling, faultEventID, mapping).Get(ctx, nil)
	default:
		remediateErr = workflow.ExecuteActivity(actCtx, activities.RemediateGPU, faultEventID, mapping).Get(ctx, nil)
	}
	if remediateErr != nil {
		logger.Warn().Err(remediateErr).Msg("failed to remediate fault, escalating")
		_ = workflow.ExecuteActivity(actCtx, activities.EscalateFault, faultEventID, "remediation failed: "+remediateErr.Error()).Get(ctx, nil)
		return remediateErr
	}

	// Step 4: ValidateRecovery — run component-specific diagnostics
	logger.Info().Msg("validating recovery")
	err = workflow.ExecuteActivity(actCtx, activities.ValidateRecovery, faultEventID, mapping).Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to validate recovery, escalating")
		_ = workflow.ExecuteActivity(actCtx, activities.EscalateFault, faultEventID, "validation failed: "+err.Error()).Get(ctx, nil)
		return err
	}

	// Step 5: RestoreService — remove maintenance mode, resolve fault and service events
	logger.Info().Msg("restoring service")
	err = workflow.ExecuteActivity(actCtx, activities.RestoreService, faultEventID).Get(ctx, nil)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to restore service, escalating")
		_ = workflow.ExecuteActivity(actCtx, activities.EscalateFault, faultEventID, "restoration failed: "+err.Error()).Get(ctx, nil)
		return err
	}

	logger.Info().Msg("completing workflow")
	return nil
}

// FaultRetentionWorkflow periodically archives resolved fault events that are
// older than the configured retention period. It runs as a long-lived Temporal
// workflow that wakes up on a timer, moves old resolved faults to the archived
// state, and then sleeps until the next cycle.
func FaultRetentionWorkflow(ctx workflow.Context, retentionDays int) error {
	logger := log.With().Str("Workflow", "FaultRetention").
		Int("RetentionDays", retentionDays).Logger()

	logger.Info().Msg("starting fault retention workflow")

	if retentionDays <= 0 {
		retentionDays = 90
	}

	retrypolicy := &temporal.RetryPolicy{
		InitialInterval:    5 * time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    5 * time.Minute,
		MaximumAttempts:    5,
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy:         retrypolicy,
	}
	actCtx := workflow.WithActivityOptions(ctx, activityOptions)

	var activities HealthActivities

	for {
		// Sleep until next retention check (24 hours)
		if err := workflow.Sleep(ctx, 24*time.Hour); err != nil {
			return err
		}

		cutoff := workflow.Now(ctx).AddDate(0, 0, -retentionDays)

		logger.Info().Time("Cutoff", cutoff).Msg("running retention sweep")

		err := workflow.ExecuteActivity(actCtx, activities.ArchiveResolvedFaults, cutoff).Get(ctx, nil)
		if err != nil {
			logger.Warn().Err(err).Msg("retention sweep failed, will retry next cycle")
		}
	}
}
