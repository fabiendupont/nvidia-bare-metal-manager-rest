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
	"encoding/json"
	"fmt"
	"net"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	providerv1 "github.com/NVIDIA/ncx-infra-controller-rest/provider/proto/nico/provider/v1"
)

// ExternalProvider wraps a gRPC connection to a provider sidecar container.
// It implements Provider and APIProvider, proxying HTTP requests over gRPC.
type ExternalProvider struct {
	info       *providerv1.ProviderInfo
	conn       *grpc.ClientConn
	client     providerv1.NicoProviderClient
	socketPath string
	routes     []*providerv1.Route
	hooks      []SyncHook
	reactions  []Reaction
	specYAML   []byte
}

// Name returns the provider's name as reported by the sidecar.
func (p *ExternalProvider) Name() string {
	return p.info.GetName()
}

// Version returns the provider's version as reported by the sidecar.
func (p *ExternalProvider) Version() string {
	return p.info.GetVersion()
}

// Features returns the features this provider offers.
func (p *ExternalProvider) Features() []string {
	return p.info.GetFeatures()
}

// Dependencies returns the providers this one depends on.
func (p *ExternalProvider) Dependencies() []string {
	return p.info.GetDependencies()
}

// Init initializes the external provider by calling the sidecar's Init RPC
// and registering any hooks or reactions it declares.
func (p *ExternalProvider) Init(ctx ProviderContext) error {
	initReq := &providerv1.InitRequest{
		TemporalNamespace: ctx.TemporalNamespace,
		TemporalQueue:     ctx.TemporalQueue,
		Config:            make(map[string]string),
	}

	resp, err := p.client.Init(context.Background(), initReq)
	if err != nil {
		return fmt.Errorf("external provider %q Init RPC failed: %w", p.Name(), err)
	}
	if !resp.GetReady() {
		return fmt.Errorf("external provider %q reported not ready: %s", p.Name(), resp.GetMessage())
	}

	// Fetch routes
	routeList, err := p.client.GetRoutes(context.Background(), &providerv1.GetRoutesRequest{})
	if err != nil {
		return fmt.Errorf("external provider %q GetRoutes RPC failed: %w", p.Name(), err)
	}
	p.routes = routeList.GetRoutes()

	// Fetch and register hooks
	if err := p.registerHooks(ctx); err != nil {
		return err
	}

	// Fetch OpenAPI fragment
	fragment, err := p.client.GetOpenAPIFragment(context.Background(), &providerv1.GetOpenAPIFragmentRequest{})
	if err != nil {
		log.Warn().Err(err).Str("provider", p.Name()).Msg("failed to fetch OpenAPI fragment")
	} else {
		p.specYAML = fragment.GetSpecYaml()
	}

	log.Info().
		Str("provider", p.Name()).
		Str("version", p.Version()).
		Int("routes", len(p.routes)).
		Int("hooks", len(p.hooks)).
		Int("reactions", len(p.reactions)).
		Msg("external provider initialized")

	return nil
}

// registerHooks fetches hook registrations from the sidecar and registers
// them with the provider registry.
func (p *ExternalProvider) registerHooks(ctx ProviderContext) error {
	regList, err := p.client.GetHookRegistrations(context.Background(), &providerv1.GetHookRegistrationsRequest{})
	if err != nil {
		log.Warn().Err(err).Str("provider", p.Name()).Msg("failed to fetch hook registrations")
		return nil
	}

	for _, reg := range regList.GetRegistrations() {
		switch reg.GetType() {
		case providerv1.HookRegistration_SYNC:
			hook := SyncHook{
				Feature: reg.GetFeature(),
				Event:   reg.GetEvent(),
				Handler: p.makeSyncHookHandler(reg.GetFeature(), reg.GetEvent()),
			}
			p.hooks = append(p.hooks, hook)
			ctx.Registry.RegisterHook(hook)

		case providerv1.HookRegistration_ASYNC:
			reaction := Reaction{
				Feature:        reg.GetFeature(),
				Event:          reg.GetEvent(),
				TargetWorkflow: reg.GetTargetWorkflow(),
				SignalName:     reg.GetSignalName(),
			}
			p.reactions = append(p.reactions, reaction)
			ctx.Registry.RegisterReaction(reaction)
		}
	}

	return nil
}

// makeSyncHookHandler creates a closure that forwards sync hook invocations
// to the sidecar over gRPC.
func (p *ExternalProvider) makeSyncHookHandler(feature, event string) func(ctx context.Context, payload interface{}) error {
	return func(ctx context.Context, payload interface{}) error {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal hook payload: %w", err)
		}

		result, err := p.client.HandleSyncHook(ctx, &providerv1.HookEvent{
			Feature: feature,
			Event:   event,
			Payload: payloadBytes,
		})
		if err != nil {
			return fmt.Errorf("external provider %q hook %s:%s RPC failed: %w", p.Name(), feature, event, err)
		}
		if !result.GetSuccess() {
			return fmt.Errorf("external provider %q hook %s:%s rejected: %s", p.Name(), feature, event, result.GetErrorMessage())
		}
		return nil
	}
}

// Shutdown calls the sidecar's Shutdown RPC and closes the gRPC connection.
func (p *ExternalProvider) Shutdown(ctx context.Context) error {
	if p.client != nil {
		if _, err := p.client.Shutdown(ctx, &providerv1.ShutdownRequest{}); err != nil {
			log.Warn().Err(err).Str("provider", p.Name()).Msg("shutdown RPC failed")
		}
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// RegisterRoutes registers proxy handlers for each route declared by the sidecar.
func (p *ExternalProvider) RegisterRoutes(group *echo.Group) {
	for _, route := range p.routes {
		method := route.GetMethod()
		path := route.GetPath()
		handler := p.proxyHandler

		switch method {
		case "GET":
			group.GET(path, handler)
		case "POST":
			group.POST(path, handler)
		case "PUT":
			group.PUT(path, handler)
		case "PATCH":
			group.PATCH(path, handler)
		case "DELETE":
			group.DELETE(path, handler)
		default:
			group.Any(path, handler)
		}

		log.Debug().
			Str("provider", p.Name()).
			Str("method", method).
			Str("path", path).
			Msg("registered external route")
	}
}

// OpenAPIFragment returns the provider's OpenAPI spec fragment as YAML.
func (p *ExternalProvider) OpenAPIFragment() []byte {
	return p.specYAML
}

// ConnectExternalProvider dials a Unix domain socket, calls GetInfo, and
// returns a fully connected ExternalProvider ready for Init.
func ConnectExternalProvider(socketPath string) (*ExternalProvider, error) {
	conn, err := grpc.NewClient(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 0)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to external provider at %s: %w", socketPath, err)
	}

	client := providerv1.NewNicoProviderClient(conn)

	info, err := client.GetInfo(context.Background(), &providerv1.GetInfoRequest{})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to get info from external provider at %s: %w", socketPath, err)
	}

	return &ExternalProvider{
		info:       info,
		conn:       conn,
		client:     client,
		socketPath: socketPath,
	}, nil
}
