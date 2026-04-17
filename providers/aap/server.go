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

package aap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	providerv1 "github.com/NVIDIA/ncx-infra-controller-rest/provider/proto/nico/provider/v1"
)

// Server implements the NicoProviderServer gRPC interface for the AAP provider.
type Server struct {
	providerv1.UnimplementedNicoProviderServer
	client   *ControllerClient
	cfg      *Config
	bindings []HookBinding
}

// NewServer creates a new AAP provider gRPC server.
func NewServer(cfg *Config) *Server {
	return &Server{
		cfg:      cfg,
		bindings: cfg.Bindings,
	}
}

// GetInfo returns the provider's metadata.
func (s *Server) GetInfo(_ context.Context, _ *providerv1.GetInfoRequest) (*providerv1.ProviderInfo, error) {
	return &providerv1.ProviderInfo{
		Name:         "nico-aap",
		Version:      "0.1.0",
		Features:     []string{},
		Dependencies: []string{"nico-compute"},
	}, nil
}

// Init initializes the provider with core runtime context.
func (s *Server) Init(_ context.Context, req *providerv1.InitRequest) (*providerv1.InitResponse, error) {
	s.client = NewControllerClient(s.cfg.ControllerURL, s.cfg.Token)

	log.Info().
		Str("controller_url", s.cfg.ControllerURL).
		Str("organization", s.cfg.Organization).
		Int("bindings", len(s.bindings)).
		Str("temporal_namespace", req.GetTemporalNamespace()).
		Msg("AAP provider initialized")

	return &providerv1.InitResponse{
		Ready:   true,
		Message: "AAP provider ready",
	}, nil
}

// Shutdown gracefully stops the provider.
func (s *Server) Shutdown(_ context.Context, _ *providerv1.ShutdownRequest) (*providerv1.ShutdownResponse, error) {
	log.Info().Msg("AAP provider shutting down")
	return &providerv1.ShutdownResponse{}, nil
}

// GetRoutes returns an empty list; the AAP provider has no REST routes.
func (s *Server) GetRoutes(_ context.Context, _ *providerv1.GetRoutesRequest) (*providerv1.RouteList, error) {
	return &providerv1.RouteList{}, nil
}

// HandleRequest returns 404; the AAP provider exposes no REST routes.
func (s *Server) HandleRequest(_ context.Context, _ *providerv1.HTTPRequest) (*providerv1.HTTPResponse, error) {
	return &providerv1.HTTPResponse{
		StatusCode: 404,
		Body:       []byte(`{"error":"AAP provider has no REST routes"}`),
		Headers:    map[string]string{"Content-Type": "application/json"},
	}, nil
}

// GetHookRegistrations returns hook registrations derived from the configured bindings.
func (s *Server) GetHookRegistrations(_ context.Context, _ *providerv1.GetHookRegistrationsRequest) (*providerv1.HookRegistrationList, error) {
	var registrations []*providerv1.HookRegistration

	for _, b := range s.bindings {
		reg := &providerv1.HookRegistration{
			Feature: b.Feature,
			Event:   b.Event,
		}

		switch b.Type {
		case "sync":
			reg.Type = providerv1.HookRegistration_SYNC
		case "async":
			reg.Type = providerv1.HookRegistration_ASYNC
			reg.TargetWorkflow = "aap-job-runner"
			reg.SignalName = fmt.Sprintf("aap-%s-%s", b.Feature, b.Event)
		default:
			reg.Type = providerv1.HookRegistration_ASYNC
		}

		registrations = append(registrations, reg)
	}

	return &providerv1.HookRegistrationList{
		Registrations: registrations,
	}, nil
}

// HandleSyncHook handles a synchronous hook invocation by launching an AAP job
// template and waiting for it to complete.
func (s *Server) HandleSyncHook(ctx context.Context, evt *providerv1.HookEvent) (*providerv1.HookResult, error) {
	binding := s.findBinding(evt.GetFeature(), evt.GetEvent())
	if binding == nil {
		return &providerv1.HookResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("no binding found for %s:%s", evt.GetFeature(), evt.GetEvent()),
		}, nil
	}

	// Deserialize payload as extra_vars
	var extraVars map[string]interface{}
	if len(evt.GetPayload()) > 0 {
		if err := json.Unmarshal(evt.GetPayload(), &extraVars); err != nil {
			return &providerv1.HookResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("failed to unmarshal hook payload: %v", err),
			}, nil
		}
	}

	input := LaunchInput{
		TemplateName: binding.TemplateName,
		Organization: s.cfg.Organization,
		ExtraVars:    extraVars,
		Timeout:      binding.Timeout,
	}

	log.Info().
		Str("template", binding.TemplateName).
		Str("feature", evt.GetFeature()).
		Str("event", evt.GetEvent()).
		Msg("launching AAP job template for sync hook")

	launchResult, err := s.client.LaunchJobTemplate(ctx, input)
	if err != nil {
		return &providerv1.HookResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("failed to launch AAP job template %q: %v", binding.TemplateName, err),
		}, nil
	}

	timeout := binding.Timeout
	if timeout == 0 {
		timeout = 15 * time.Minute
	}

	log.Info().
		Int("job_id", launchResult.JobID).
		Str("template", binding.TemplateName).
		Msg("waiting for AAP job to complete")

	jobResult, err := s.client.WaitForJob(ctx, launchResult.JobID, timeout)
	if err != nil {
		return &providerv1.HookResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("AAP job %d wait failed: %v", launchResult.JobID, err),
		}, nil
	}

	if jobResult.Status == "successful" {
		log.Info().
			Int("job_id", jobResult.ID).
			Float64("elapsed", jobResult.Elapsed).
			Msg("AAP job completed successfully")
		return &providerv1.HookResult{
			Success: true,
		}, nil
	}

	log.Warn().
		Int("job_id", jobResult.ID).
		Str("status", jobResult.Status).
		Bool("failed", jobResult.Failed).
		Msg("AAP job did not succeed")

	return &providerv1.HookResult{
		Success:      false,
		ErrorMessage: fmt.Sprintf("AAP job %d failed: %s", jobResult.ID, jobResult.Status),
	}, nil
}

// GetOpenAPIFragment returns an empty fragment; the AAP provider has no REST routes.
func (s *Server) GetOpenAPIFragment(_ context.Context, _ *providerv1.GetOpenAPIFragmentRequest) (*providerv1.OpenAPIFragment, error) {
	return &providerv1.OpenAPIFragment{}, nil
}

// findBinding returns the first binding matching the given feature and event.
func (s *Server) findBinding(feature, event string) *HookBinding {
	for i := range s.bindings {
		if s.bindings[i].Feature == feature && s.bindings[i].Event == event {
			return &s.bindings[i]
		}
	}
	return nil
}
