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

// BlueprintHandler handles blueprint API requests.
type BlueprintHandler struct {
	store BlueprintStoreInterface
}

// NewBlueprintHandler creates a new handler.
func NewBlueprintHandler(store BlueprintStoreInterface) *BlueprintHandler {
	return &BlueprintHandler{store: store}
}

func (h *BlueprintHandler) handleCreateBlueprint(c echo.Context) error {
	var b Blueprint
	if err := c.Bind(&b); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid_request", "message": "failed to parse request body"})
	}

	// Derive tenant_id from auth context — callers cannot set arbitrary tenant_id
	if callerTenant := c.Get("tenant_id"); callerTenant != nil {
		if tid, ok := callerTenant.(string); ok && tid != "" {
			parsed, err := uuid.Parse(tid)
			if err == nil {
				b.TenantID = &parsed
			}
		}
	}

	// Set default visibility
	if b.Visibility == "" {
		if b.TenantID != nil {
			b.Visibility = VisibilityOrganization
		} else {
			b.Visibility = VisibilityPublic
		}
	}

	// Validate visibility value
	if b.Visibility != VisibilityPublic && b.Visibility != VisibilityOrganization && b.Visibility != VisibilityPrivate {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "validation_error", "message": "visibility must be public, organization, or private"})
	}

	// Validate pricing if provided
	if b.Pricing != nil {
		if b.Pricing.Rate < 0 {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "validation_error", "message": "pricing rate must be non-negative"})
		}
		if b.Pricing.Unit == "" {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "validation_error", "message": "pricing unit is required (hour, month, one-time)"})
		}
		if b.Pricing.Currency == "" {
			b.Pricing.Currency = "USD"
		}
	}

	// Validate based_on reference if provided
	if b.BasedOn != "" {
		parent, err := lookupBlueprint(b.BasedOn, h.store)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "validation_error", "message": "based_on references a blueprint that does not exist"})
		}
		// If no resources defined, the variant inherits the parent as a single-node DAG
		if len(b.Resources) == 0 {
			b.Resources = map[string]BlueprintResource{
				"base": {
					Type: "blueprint/" + parent.Name,
				},
			}
		}
	}

	result := ValidateBlueprint(&b)
	if !result.Valid {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "validation_failed", "message": "Blueprint validation failed", "details": result.Errors})
	}

	if err := h.store.Create(&b); err != nil {
		return c.JSON(http.StatusConflict, echo.Map{"error": "conflict", "message": err.Error()})
	}

	return c.JSON(http.StatusCreated, b)
}

func (h *BlueprintHandler) handleListBlueprints(c echo.Context) error {
	blueprints := h.store.GetAll()
	if blueprints == nil {
		blueprints = []*Blueprint{}
	}

	// Filter by tenant visibility if tenant_id query param is provided
	tenantParam := c.QueryParam("tenant_id")
	if tenantParam != "" {
		tenantID, err := uuid.Parse(tenantParam)
		if err != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid_id", "message": "invalid tenant_id"})
		}
		var filtered []*Blueprint
		for _, bp := range blueprints {
			// Include: provider-published (public) OR same tenant's blueprints
			if bp.TenantID == nil && bp.Visibility == VisibilityPublic {
				filtered = append(filtered, bp)
			} else if bp.TenantID != nil && *bp.TenantID == tenantID {
				filtered = append(filtered, bp)
			}
		}
		blueprints = filtered
	}

	offset, limit := provider.ParsePagination(c)
	total := len(blueprints)
	start, end := provider.Paginate(total, offset, limit)
	page := blueprints[start:end]
	if page == nil {
		page = []*Blueprint{}
	}

	return c.JSON(http.StatusOK, provider.ListResponse{
		Items:  page,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	})
}

func (h *BlueprintHandler) handleGetBlueprint(c echo.Context) error {
	id := c.Param("id")
	b, err := h.store.GetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not_found", "message": err.Error()})
	}

	// Tenant isolation: private/org-scoped blueprints only visible to their owner
	if b.TenantID != nil && b.Visibility != VisibilityPublic {
		callerTenant := c.Get("tenant_id")
		if callerTenant == nil || callerTenant.(string) != b.TenantID.String() {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "not_found", "message": "blueprint not found"})
		}
	}

	return c.JSON(http.StatusOK, b)
}

func (h *BlueprintHandler) handleUpdateBlueprint(c echo.Context) error {
	id := c.Param("id")
	existing, err := h.store.GetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not_found", "message": err.Error()})
	}

	// Ownership check: tenant-owned blueprints can only be updated by their owner
	if existing.TenantID != nil {
		callerTenant := c.Get("tenant_id")
		if callerTenant == nil || callerTenant.(string) != existing.TenantID.String() {
			return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden", "message": "cannot modify another tenant's blueprint"})
		}
	}

	var update Blueprint
	if err := c.Bind(&update); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid_request", "message": err.Error()})
	}

	if update.Name != "" {
		existing.Name = update.Name
	}
	if update.Version != "" {
		existing.Version = update.Version
	}
	if update.Description != "" {
		existing.Description = update.Description
	}
	if update.Parameters != nil {
		existing.Parameters = update.Parameters
	}
	if update.Resources != nil {
		existing.Resources = update.Resources
	}
	if update.Labels != nil {
		existing.Labels = update.Labels
	}
	if update.Pricing != nil {
		existing.Pricing = update.Pricing
	}
	if update.Visibility != "" {
		existing.Visibility = update.Visibility
	}

	result := ValidateBlueprint(existing)
	if !result.Valid {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "validation_failed", "message": "Updated blueprint is invalid", "details": result.Errors})
	}

	if err := h.store.Update(existing); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "update_failed", "message": err.Error()})
	}

	return c.JSON(http.StatusOK, existing)
}

func (h *BlueprintHandler) handleDeleteBlueprint(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.Delete(id); err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not_found", "message": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *BlueprintHandler) handleValidateBlueprint(c echo.Context) error {
	id := c.Param("id")
	b, err := h.store.GetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not_found", "message": err.Error()})
	}
	result := ValidateBlueprint(b)
	return c.JSON(http.StatusOK, result)
}

// handleResolvedBlueprint returns the effective blueprint after variant resolution.
// Locked parameters are excluded from the response.
// GET /catalog/blueprints/:id/resolved
func (h *BlueprintHandler) handleResolvedBlueprint(c echo.Context) error {
	id := c.Param("id")
	b, err := h.store.GetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not_found", "message": err.Error()})
	}

	resolved, err := ResolveBlueprint(b, h.store)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "resolve_failed", "message": err.Error()})
	}

	// Filter out locked parameters — they are enforced but not shown in the ordering form.
	resolved.Parameters = FilterUnlockedParameters(resolved.Parameters)

	return c.JSON(http.StatusOK, resolved)
}

func (h *BlueprintHandler) handleListResourceTypes(c echo.Context) error {
	return c.JSON(http.StatusOK, echo.Map{"resource_types": AvailableResourceTypes})
}

// handleEstimateCost returns a cost estimate for a blueprint with given parameters.
// POST /catalog/blueprints/:id/estimate
func (h *BlueprintHandler) handleEstimateCost(c echo.Context) error {
	id := c.Param("id")
	b, err := h.store.GetByID(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "not_found", "message": err.Error()})
	}

	// If the blueprint has explicit pricing, return it directly
	if b.Pricing != nil {
		return c.JSON(http.StatusOK, CostEstimate{
			EstimatedRate: b.Pricing.Rate,
			Unit:          b.Pricing.Unit,
			Currency:      b.Pricing.Currency,
			Breakdown:     []CostBreakdownItem{{Blueprint: b.Name, Rate: b.Pricing.Rate}},
		})
	}

	// Walk the DAG to sum pricing from constituent blueprints
	var totalRate float64
	var breakdown []CostBreakdownItem
	unit := "hour"
	currency := "USD"

	for _, res := range b.Resources {
		ref := extractBlueprintRef(res.Type)
		if ref == "" {
			continue
		}
		child, err := lookupBlueprint(ref, h.store)
		if err != nil {
			continue
		}
		if child.Pricing != nil {
			totalRate += child.Pricing.Rate
			unit = child.Pricing.Unit
			currency = child.Pricing.Currency
			breakdown = append(breakdown, CostBreakdownItem{
				Blueprint: child.Name,
				Rate:      child.Pricing.Rate,
			})
		}
	}

	return c.JSON(http.StatusOK, CostEstimate{
		EstimatedRate: totalRate,
		Unit:          unit,
		Currency:      currency,
		Breakdown:     breakdown,
	})
}

// CostEstimate represents the estimated cost for a blueprint.
type CostEstimate struct {
	EstimatedRate float64             `json:"estimated_rate"`
	Unit          string              `json:"unit"`
	Currency      string              `json:"currency"`
	Breakdown     []CostBreakdownItem `json:"breakdown"`
}

// CostBreakdownItem shows the cost contribution of a sub-blueprint.
type CostBreakdownItem struct {
	Blueprint string  `json:"blueprint"`
	Rate      float64 `json:"rate"`
}

// extractBlueprintRef returns the raw reference from a "blueprint/..." resource type.
// Returns empty string if not a blueprint type.
func extractBlueprintRef(resType string) string {
	if len(resType) > 10 && resType[:10] == "blueprint/" {
		return resType[10:]
	}
	return ""
}

// parseRef splits a blueprint reference like "name@1.0.0" or a UUID
// into its name/ID and optional version parts.
func parseRef(ref string) (nameOrID, version string) {
	for i, c := range ref {
		if c == '@' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, ""
}

// lookupBlueprint resolves a blueprint reference (from a "blueprint/..." resource type)
// against the store. Tries by UUID first, then by name+version.
func lookupBlueprint(ref string, store BlueprintStoreInterface) (*Blueprint, error) {
	nameOrID, version := parseRef(ref)

	// Try by ID first (works for UUID references)
	if b, err := store.GetByID(nameOrID); err == nil {
		return b, nil
	}

	// Fall back to name+version lookup
	return store.GetByNameVersion(nameOrID, version)
}
