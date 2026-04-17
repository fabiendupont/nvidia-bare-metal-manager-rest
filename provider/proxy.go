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
	"io"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"

	providerv1 "github.com/NVIDIA/ncx-infra-controller-rest/provider/proto/nico/provider/v1"
)

// proxyHandler forwards an HTTP request from the Echo context to the external
// provider sidecar via gRPC HandleRequest and writes the response back.
func (p *ExternalProvider) proxyHandler(c echo.Context) error {
	req := c.Request()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{
			"error": "failed to read request body",
		})
	}

	grpcReq := &providerv1.HTTPRequest{
		Method:      req.Method,
		Path:        req.URL.Path,
		Headers:     extractHeaders(req),
		QueryParams: extractQueryParams(req.URL.Query()),
		PathParams:  extractPathParams(c),
		Body:        body,
		Org:         c.Param("orgName"),
		TenantId:    getContextString(c, "tenant_id"),
		UserId:      getContextString(c, "user_id"),
		Roles:       extractRoles(c),
	}

	grpcResp, err := p.client.HandleRequest(c.Request().Context(), grpcReq)
	if err != nil {
		log.Error().Err(err).
			Str("provider", p.Name()).
			Str("method", req.Method).
			Str("path", req.URL.Path).
			Msg("external provider HandleRequest failed")
		return c.JSON(http.StatusBadGateway, map[string]string{
			"error":    "external_provider_error",
			"provider": p.Name(),
			"message":  err.Error(),
		})
	}

	// Copy response headers
	for k, v := range grpcResp.GetHeaders() {
		c.Response().Header().Set(k, v)
	}

	return c.Blob(int(grpcResp.GetStatusCode()), c.Response().Header().Get("Content-Type"), grpcResp.GetBody())
}

// extractHeaders converts HTTP headers to a flat string map.
// For headers with multiple values, only the first value is used.
func extractHeaders(r *http.Request) map[string]string {
	headers := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	return headers
}

// extractQueryParams converts URL query values to a flat string map.
// For parameters with multiple values, only the first value is used.
func extractQueryParams(values url.Values) map[string]string {
	params := make(map[string]string, len(values))
	for k, v := range values {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	return params
}

// extractPathParams extracts Echo path parameters into a string map.
func extractPathParams(c echo.Context) map[string]string {
	names := c.ParamNames()
	params := make(map[string]string, len(names))
	for _, name := range names {
		params[name] = c.Param(name)
	}
	return params
}

// getContextString retrieves a string value from the Echo context.
func getContextString(c echo.Context, key string) string {
	val := c.Get(key)
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// extractRoles retrieves the roles list from the Echo context.
func extractRoles(c echo.Context) []string {
	val := c.Get("roles")
	if val == nil {
		return nil
	}
	if roles, ok := val.([]string); ok {
		return roles
	}
	return nil
}
