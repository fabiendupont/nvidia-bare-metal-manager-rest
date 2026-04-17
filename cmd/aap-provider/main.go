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

package main

import (
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"

	providerv1 "github.com/NVIDIA/ncx-infra-controller-rest/provider/proto/nico/provider/v1"
	"github.com/NVIDIA/ncx-infra-controller-rest/providers/aap"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	socketPath := os.Getenv("NICO_PROVIDER_SOCKET")
	if socketPath == "" {
		socketPath = "/var/run/nico/providers/nico-aap.sock"
	}

	cfg, err := aap.LoadConfigFromEnv()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load AAP provider config")
	}

	server := aap.NewServer(cfg)

	// Ensure the socket directory exists
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		log.Fatal().Err(err).Str("path", filepath.Dir(socketPath)).Msg("failed to create socket directory")
	}

	// Remove stale socket file
	os.Remove(socketPath)

	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatal().Err(err).Str("socket", socketPath).Msg("failed to listen on unix socket")
	}

	grpcServer := grpc.NewServer()
	providerv1.RegisterNicoProviderServer(grpcServer, server)

	// Graceful shutdown on SIGTERM/SIGINT
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("received signal, shutting down")
		grpcServer.GracefulStop()
	}()

	log.Info().Str("socket", socketPath).Msg("AAP provider listening")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal().Err(err).Msg("gRPC server failed")
	}
}
