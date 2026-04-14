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
	"net/http"
	"os"

	cdbm "github.com/NVIDIA/ncx-infra-controller-rest/db/pkg/db/model"
	echo "github.com/labstack/echo/v4"
)

const (
	RoleProviderAdmin   = "FORGE_PROVIDER_ADMIN"
	RoleTenantAdmin     = "FORGE_TENANT_ADMIN"
	RoleBlueprintAuthor = "BLUEPRINT_AUTHOR"
)

// allowAnonymous returns true only when NICO_AUTH_MODE=dev is explicitly set.
// In production (default), a missing user always results in 401.
func allowAnonymous() bool {
	return os.Getenv("NICO_AUTH_MODE") == "dev"
}

// GetUser extracts the authenticated user from the echo context.
func GetUser(c echo.Context) *cdbm.User {
	u, _ := c.Get("user").(*cdbm.User)
	return u
}

// GetOrgName extracts the organization name from the echo context.
func GetOrgName(c echo.Context) string {
	org, _ := c.Get("orgName").(string)
	return org
}

// GetTenantID extracts the tenant_id string from the echo context.
func GetTenantID(c echo.Context) string {
	tid, _ := c.Get("tenant_id").(string)
	return tid
}

// RequireRole returns an echo middleware that checks the user has one of
// the specified roles for the current org. Returns 401 if no user is set,
// unless NICO_AUTH_MODE=dev.
func RequireRole(roles ...string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				if allowAnonymous() {
					return next(c)
				}
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized", "message": "authentication required"})
			}

			org := GetOrgName(c)
			if org == "" {
				return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden", "message": "organization context required"})
			}

			orgDetails, err := user.OrgData.GetOrgByName(org)
			if err != nil {
				return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden", "message": "not a member of this organization"})
			}

			roleMap := make(map[string]bool, len(roles))
			for _, r := range roles {
				roleMap[r] = true
			}

			for _, userRole := range orgDetails.Roles {
				if roleMap[userRole] {
					return next(c)
				}
			}

			return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden", "message": "insufficient permissions"})
		}
	}
}

// RequireAuth returns an echo middleware that checks a user is authenticated
// and is a member of the current org. Returns 401 if no user is set,
// unless NICO_AUTH_MODE=dev.
func RequireAuth() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				if allowAnonymous() {
					return next(c)
				}
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized", "message": "authentication required"})
			}

			org := GetOrgName(c)
			if org == "" {
				return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden", "message": "organization context required"})
			}

			_, err := user.OrgData.GetOrgByName(org)
			if err != nil {
				return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden", "message": "not a member of this organization"})
			}

			return next(c)
		}
	}
}
