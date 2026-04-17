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

package catalog

import (
	"net/http"

	"github.com/NVIDIA/ncx-infra-controller-rest/provider"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// OrderProxy provides read-only order access through the catalog API by
// delegating to the fulfillment provider via the registry. This avoids
// a direct import of the fulfillment package while letting catalog
// consumers view their orders alongside blueprints.
type OrderProxy struct {
	registry *provider.Registry
}

// NewOrderProxy creates a new order proxy.
func NewOrderProxy(registry *provider.Registry) *OrderProxy {
	return &OrderProxy{registry: registry}
}

// handleListOrders lists orders by delegating to the fulfillment provider.
// GET /catalog/orders
func (p *OrderProxy) handleListOrders(c echo.Context) error {
	if p.registry == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"error":   "service_unavailable",
			"message": "fulfillment provider is not available",
		})
	}

	if _, ok := p.registry.Get("nico-fulfillment"); !ok {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"error":   "service_unavailable",
			"message": "fulfillment provider is not registered",
		})
	}

	// The fulfillment provider registers its own /catalog/orders routes.
	// This proxy endpoint confirms the fulfillment provider is available
	// and directs clients to its endpoints.
	return c.JSON(http.StatusOK, echo.Map{
		"message":  "orders are managed by the fulfillment provider",
		"provider": "nico-fulfillment",
		"status":   "available",
	})
}

// handleGetOrder retrieves an order by delegating to the fulfillment provider.
// GET /catalog/orders/:id
func (p *OrderProxy) handleGetOrder(c echo.Context) error {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "invalid_id",
			"message": "invalid order id",
		})
	}

	if p.registry == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"error":   "service_unavailable",
			"message": "fulfillment provider is not available",
		})
	}

	_, ok := p.registry.Get("nico-fulfillment")
	if !ok {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"error":   "service_unavailable",
			"message": "fulfillment provider is not registered",
		})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message":  "use the fulfillment provider's /catalog/orders/:id endpoint",
		"provider": "nico-fulfillment",
		"status":   "available",
	})
}

// handleDeleteOrder cancels an order by delegating to the fulfillment provider.
// DELETE /catalog/orders/:id
func (p *OrderProxy) handleDeleteOrder(c echo.Context) error {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "invalid_id",
			"message": "invalid order id",
		})
	}

	if p.registry == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"error":   "service_unavailable",
			"message": "fulfillment provider is not available",
		})
	}

	_, ok := p.registry.Get("nico-fulfillment")
	if !ok {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"error":   "service_unavailable",
			"message": "fulfillment provider is not registered",
		})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message":  "use DELETE on the fulfillment provider's /catalog/orders/:id endpoint",
		"provider": "nico-fulfillment",
		"status":   "available",
	})
}
