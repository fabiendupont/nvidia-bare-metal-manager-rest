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
	"time"

	echo "github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// WebhookHandler groups webhook ingestion HTTP handlers for external alert
// sources such as AlertManager.
type WebhookHandler struct {
	faultStore          *FaultEventStore
	classificationStore *ClassificationStore
}

// NewWebhookHandler creates a WebhookHandler with the given stores.
func NewWebhookHandler(
	faultStore *FaultEventStore,
	classificationStore *ClassificationStore,
) *WebhookHandler {
	return &WebhookHandler{
		faultStore:          faultStore,
		classificationStore: classificationStore,
	}
}

// AlertManagerPayload represents the webhook payload sent by Prometheus
// AlertManager. See https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
type AlertManagerPayload struct {
	Version     string               `json:"version"`
	GroupKey    string               `json:"groupKey"`
	Status      string               `json:"status"`
	Receiver    string               `json:"receiver"`
	Alerts      []AlertManagerAlert  `json:"alerts"`
	CommonLabels map[string]string   `json:"commonLabels,omitempty"`
}

// AlertManagerAlert represents a single alert within the AlertManager payload.
type AlertManagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	Fingerprint  string            `json:"fingerprint,omitempty"`
}

// handleAlertManagerWebhook handles POST /health/webhooks/alertmanager.
// It parses the AlertManager payload and creates FaultEvent entries for each
// firing alert. Resolved alerts are ignored (handled by the remediation
// workflow).
func (h *WebhookHandler) handleAlertManagerWebhook(c echo.Context) error {
	logger := log.With().Str("Handler", "AlertManagerWebhook").Logger()

	var payload AlertManagerPayload
	if err := c.Bind(&payload); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "bad_request",
			"message": "Failed to parse AlertManager payload",
		})
	}

	if len(payload.Alerts) == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":   "validation_error",
			"message": "No alerts in payload",
		})
	}

	var created []*FaultEvent
	for _, alert := range payload.Alerts {
		// Only process firing alerts; resolved alerts are handled by
		// the remediation workflow.
		if alert.Status != "firing" {
			continue
		}

		severity := mapAlertManagerSeverity(alert.Labels["severity"])
		component := alert.Labels["component"]
		if component == "" {
			component = "unknown"
		}

		message := alert.Annotations["summary"]
		if message == "" {
			message = alert.Labels["alertname"]
		}

		classification := alert.Labels["classification"]
		var classificationPtr *string
		if classification != "" {
			classificationPtr = &classification
		}

		machineID := alert.Labels["machine_id"]
		var machineIDPtr *string
		if machineID != "" {
			machineIDPtr = &machineID
		}

		siteID := alert.Labels["site_id"]

		metadata := make(map[string]interface{})
		metadata["alertmanager_fingerprint"] = alert.Fingerprint
		metadata["alertmanager_generator_url"] = alert.GeneratorURL
		for k, v := range alert.Labels {
			metadata["label_"+k] = v
		}

		event := &FaultEvent{
			Source:         "alertmanager",
			Severity:       severity,
			Component:      component,
			Classification: classificationPtr,
			Message:        message,
			MachineID:      machineIDPtr,
			SiteID:         siteID,
			State:          FaultStateOpen,
			DetectedAt:     alert.StartsAt,
			Metadata:       metadata,
		}

		if err := h.faultStore.Create(event); err != nil {
			logger.Warn().Err(err).Str("Fingerprint", alert.Fingerprint).
				Msg("failed to create fault event from alert")
			continue
		}

		created = append(created, event)
		logger.Info().Str("FaultID", event.ID).
			Str("AlertName", alert.Labels["alertname"]).
			Msg("fault event created from AlertManager alert")
	}

	return c.JSON(http.StatusCreated, echo.Map{
		"created": len(created),
		"events":  created,
	})
}

// mapAlertManagerSeverity maps AlertManager severity labels to NICo fault
// severities. Unknown values default to "warning".
func mapAlertManagerSeverity(s string) string {
	switch s {
	case "critical":
		return SeverityCritical
	case "warning":
		return SeverityWarning
	case "info", "none":
		return SeverityInfo
	default:
		return SeverityWarning
	}
}
