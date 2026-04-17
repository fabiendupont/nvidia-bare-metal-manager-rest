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
	"net/http"

	echo "github.com/labstack/echo/v4"
)

// ClassificationHandler groups the operator-facing classification management
// HTTP handlers and the store they depend on.
type ClassificationHandler struct {
	classificationStore *ClassificationStore
}

// NewClassificationHandler creates a ClassificationHandler with the given store.
func NewClassificationHandler(classificationStore *ClassificationStore) *ClassificationHandler {
	return &ClassificationHandler{
		classificationStore: classificationStore,
	}
}

// handleListClassifications handles GET /health/classifications.
// Returns all classification-to-remediation mappings.
func (h *ClassificationHandler) handleListClassifications(c echo.Context) error {
	mappings := h.classificationStore.GetAll()
	return c.JSON(http.StatusOK, mappings)
}

// handleUpdateClassification handles PUT /health/classifications/:classification.
// Creates or replaces a classification-to-remediation mapping.
func (h *ClassificationHandler) handleUpdateClassification(c echo.Context) error {
	classification := c.Param("classification")
	if classification == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "validation_error",
			"message": "Classification name is required",
		})
	}

	var mapping ClassificationMapping
	if err := c.Bind(&mapping); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "bad_request",
			"message": "Failed to parse request body",
		})
	}

	if mapping.Component == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "validation_error",
			"message": "Component is required",
		})
	}
	if mapping.Severity == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "validation_error",
			"message": "Severity is required",
		})
	}
	if mapping.Remediation == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "validation_error",
			"message": "Remediation is required",
		})
	}

	mapping.Classification = classification
	h.classificationStore.Set(classification, &mapping)

	return c.JSON(http.StatusOK, &mapping)
}
