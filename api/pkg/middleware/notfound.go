// SPDX-FileCopyrightText: Copyright (c) 2021-2023 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: LicenseRef-NvidiaProprietary
//
// NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
// property and proprietary rights in and to this material, related
// documentation and any modifications thereto. Any use, reproduction,
// disclosure or distribution of this material and related documentation
// without an express license agreement from NVIDIA CORPORATION or
// its affiliates is strictly prohibited.

package middleware

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nvidia/carbide-rest/api/internal/config"
	ccu "github.com/nvidia/carbide-rest/common/pkg/util"
)

// NotFoundHandler returns a middleware that returns a 404 status code for unmatched routes
func NotFoundHandler(cfg *config.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth processing for unmatched path
			if c.Path() == fmt.Sprintf("/%s/*", cfg.GetAPIRouteVersion()) {
				return ccu.NewAPIErrorResponse(c, http.StatusNotFound, "The requested path could not be found", nil)
			}

			return next(c)
		}
	}
}
