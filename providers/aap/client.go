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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ControllerClient wraps the AAP Controller REST API.
type ControllerClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// LaunchInput describes the parameters for launching a job template.
type LaunchInput struct {
	TemplateName string
	Organization string
	ExtraVars    map[string]interface{}
	Limit        string
	Timeout      time.Duration
}

// LaunchOutput describes the result of launching a job template.
type LaunchOutput struct {
	JobID  int    `json:"id"`
	Status string `json:"status"`
}

// JobResult describes the final state of a completed job.
type JobResult struct {
	ID      int     `json:"id"`
	Status  string  `json:"status"`
	Failed  bool    `json:"failed"`
	Elapsed float64 `json:"elapsed"`
}

// NewControllerClient creates a new AAP Controller REST client.
func NewControllerClient(baseURL, token string) *ControllerClient {
	return &ControllerClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// templateListResponse is the AAP paginated response for job template queries.
type templateListResponse struct {
	Results []struct {
		ID int `json:"id"`
	} `json:"results"`
}

// resolveTemplateID looks up a job template by name and returns its ID.
func (c *ControllerClient) resolveTemplateID(ctx context.Context, name string) (int, error) {
	u := fmt.Sprintf("%s/api/v2/job_templates/?name=%s", c.baseURL, url.QueryEscape(name))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request job templates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return 0, fmt.Errorf("AAP authentication failed (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("AAP returned status %d looking up template %q", resp.StatusCode, name)
	}

	var list templateListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return 0, fmt.Errorf("decode template list: %w", err)
	}
	if len(list.Results) == 0 {
		return 0, fmt.Errorf("job template %q not found", name)
	}

	return list.Results[0].ID, nil
}

// LaunchJobTemplate starts a job from a named template.
func (c *ControllerClient) LaunchJobTemplate(ctx context.Context, input LaunchInput) (*LaunchOutput, error) {
	templateID, err := c.resolveTemplateID(ctx, input.TemplateName)
	if err != nil {
		return nil, fmt.Errorf("resolve template: %w", err)
	}

	body := make(map[string]interface{})
	if len(input.ExtraVars) > 0 {
		varsJSON, err := json.Marshal(input.ExtraVars)
		if err != nil {
			return nil, fmt.Errorf("marshal extra_vars: %w", err)
		}
		body["extra_vars"] = string(varsJSON)
	}
	if input.Limit != "" {
		body["limit"] = input.Limit
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal launch body: %w", err)
	}

	u := fmt.Sprintf("%s/api/v2/job_templates/%d/launch/", c.baseURL, templateID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build launch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("launch request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("AAP authentication failed (401)")
	}
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("AAP returned status %d launching template %q: %s", resp.StatusCode, input.TemplateName, string(respBody))
	}

	var out LaunchOutput
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode launch response: %w", err)
	}

	return &out, nil
}

// WaitForJob polls until the job reaches a terminal state.
func (c *ControllerClient) WaitForJob(ctx context.Context, jobID int, timeout time.Duration) (*JobResult, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		result, err := c.getJobStatus(ctx, jobID)
		if err != nil {
			return nil, err
		}

		if isTerminalStatus(result.Status) {
			return result, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("AAP job %d timed out after %s (last status: %s)", jobID, timeout, result.Status)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// getJobStatus retrieves the current status of a job.
func (c *ControllerClient) getJobStatus(ctx context.Context, jobID int) (*JobResult, error) {
	u := fmt.Sprintf("%s/api/v2/jobs/%d/", c.baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build job status request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request job status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("AAP authentication failed (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AAP returned status %d for job %d", resp.StatusCode, jobID)
	}

	var result JobResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode job status: %w", err)
	}

	return &result, nil
}

// GetJobOutput retrieves stdout from a completed job.
func (c *ControllerClient) GetJobOutput(ctx context.Context, jobID int) (string, error) {
	u := fmt.Sprintf("%s/api/v2/jobs/%d/stdout/?format=txt", c.baseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("build job output request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request job output: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("AAP authentication failed (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AAP returned status %d for job %d stdout", resp.StatusCode, jobID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read job output: %w", err)
	}

	return string(body), nil
}

// isTerminalStatus returns true if the AAP job status is terminal.
func isTerminalStatus(status string) bool {
	switch status {
	case "successful", "failed", "error", "canceled":
		return true
	default:
		return false
	}
}
