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

package carbide

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	cli "github.com/urfave/cli/v2"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token" yaml:"access-token"`
	RefreshToken string `json:"refresh_token" yaml:"refresh-token"`
	ExpiresIn    int    `json:"expires_in" yaml:"expires-in"`
	TokenType    string `json:"token_type" yaml:"token-type"`
}

type ConfigFile struct {
	BaseURL      string `yaml:"base-url,omitempty"`
	Org          string `yaml:"org,omitempty"`
	Token        string `yaml:"token,omitempty"`
	RefreshToken string `yaml:"refresh-token,omitempty"`
	ExpiresAt    string `yaml:"expires-at,omitempty"`
	TokenURL     string `yaml:"token-url,omitempty"`
	KeycloakURL  string `yaml:"keycloak-url,omitempty"`
	Realm        string `yaml:"realm,omitempty"`
	ClientID     string `yaml:"client-id,omitempty"`
	ClientSecret string `yaml:"client-secret,omitempty"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".carbide.yaml")
}

func LoadConfig() (*ConfigFile, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &ConfigFile{}, nil
		}
		return nil, err
	}
	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig writes the config file, preserving any unknown keys that may
// have been manually added by the user.
func SaveConfig(cfg *ConfigFile) error {
	// Load existing file as a raw map to preserve unknown keys.
	raw := make(map[string]interface{})
	if data, err := os.ReadFile(configPath()); err == nil {
		yaml.Unmarshal(data, &raw)
	}

	// Merge known fields into the map, omitting empty values.
	setOrDelete(raw, "base-url", cfg.BaseURL)
	setOrDelete(raw, "org", cfg.Org)
	setOrDelete(raw, "token", cfg.Token)
	setOrDelete(raw, "refresh-token", cfg.RefreshToken)
	setOrDelete(raw, "expires-at", cfg.ExpiresAt)
	setOrDelete(raw, "token-url", cfg.TokenURL)
	setOrDelete(raw, "keycloak-url", cfg.KeycloakURL)
	setOrDelete(raw, "realm", cfg.Realm)
	setOrDelete(raw, "client-id", cfg.ClientID)
	setOrDelete(raw, "client-secret", cfg.ClientSecret)

	data, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(configPath(), data, 0600)
}

func setOrDelete(m map[string]interface{}, key, value string) {
	if value != "" {
		m[key] = value
	} else {
		delete(m, key)
	}
}

func LoginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "Authenticate with the Carbide API",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "username",
				Usage: "Username for OIDC password grant login",
			},
			&cli.StringFlag{
				Name:  "password",
				Usage: "Password for OIDC password grant login (prompted if not provided)",
			},
			&cli.StringFlag{
				Name:    "client-secret",
				Usage:   "Client secret (required for confidential OIDC clients)",
				EnvVars: []string{"CARBIDE_CLIENT_SECRET"},
			},
			&cli.StringFlag{
				Name:    "api-key",
				Usage:   "NGC API key for token exchange",
				EnvVars: []string{"CARBIDE_API_KEY"},
			},
			&cli.StringFlag{
				Name:    "authn-url",
				Usage:   "NGC authentication URL for API key exchange",
				EnvVars: []string{"CARBIDE_AUTHN_URL"},
				Value:   "https://authn.nvidia.com/token",
			},
		},
		Action: func(c *cli.Context) error {
			apiKey := c.String("api-key")

			if apiKey != "" {
				return loginWithAPIKey(c, apiKey)
			}
			return loginWithOIDC(c)
		},
	}
}

func loginWithAPIKey(c *cli.Context, apiKey string) error {
	authnURL := c.String("authn-url")

	req, err := http.NewRequest("GET", authnURL, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "ApiKey "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("requesting token from NGC: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("NGC token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// NGC responds with {"token": "..."} or {"access_token": "..."}
	token := extractNGCToken(body)
	if token == "" {
		return fmt.Errorf("NGC response did not contain a token")
	}

	cfg, _ := LoadConfig()
	cfg.Token = token
	cfg.RefreshToken = ""
	cfg.ExpiresAt = ""

	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Login successful (NGC API key). Token saved to %s\n", configPath())
	return nil
}

func extractNGCToken(body []byte) string {
	var resp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return ""
	}
	if resp.Token != "" {
		return resp.Token
	}
	return resp.AccessToken
}

func loginWithOIDC(c *cli.Context) error {
	clientID := c.String("client-id")
	clientSecret := c.String("client-secret")
	username := c.String("username")
	password := c.String("password")

	tokenURL := resolveTokenURL(c)
	if tokenURL == "" {
		return fmt.Errorf("--token-url or --keycloak-url is required for login")
	}

	var tokenResp *TokenResponse
	var err error

	if username == "" && clientSecret != "" {
		tokenResp, err = clientCredentialsGrant(tokenURL, clientID, clientSecret)
	} else {
		if username == "" {
			fmt.Print("Username: ")
			var u string
			fmt.Scanln(&u)
			username = u
		}
		if password == "" {
			fmt.Print("Password: ")
			pw, readErr := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if readErr != nil {
				return fmt.Errorf("reading password: %w", readErr)
			}
			password = string(pw)
		}
		tokenResp, err = passwordGrant(tokenURL, clientID, clientSecret, username, password)
	}
	if err != nil {
		return err
	}

	cfg, _ := LoadConfig()
	cfg.Token = tokenResp.AccessToken
	cfg.RefreshToken = tokenResp.RefreshToken
	cfg.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)
	cfg.TokenURL = tokenURL
	cfg.ClientID = clientID
	cfg.ClientSecret = clientSecret

	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Login successful. Token saved to %s\n", configPath())
	return nil
}

func passwordGrant(tokenURL, clientID, clientSecret, username, password string) (*TokenResponse, error) {
	data := url.Values{
		"grant_type": {"password"},
		"client_id":  {clientID},
		"username":   {username},
		"password":   {password},
		"scope":      {"openid"},
	}
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}
	return postToken(tokenURL, data)
}

func clientCredentialsGrant(tokenURL, clientID, clientSecret string) (*TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"scope":         {"openid"},
	}
	return postToken(tokenURL, data)
}

// resolveTokenURL determines the OIDC token endpoint URL.
// --token-url takes precedence; if not set, constructs from --keycloak-url + --realm.
func resolveTokenURL(c *cli.Context) string {
	if u := c.String("token-url"); u != "" {
		return u
	}
	if keycloakURL := c.String("keycloak-url"); keycloakURL != "" {
		realm := c.String("realm")
		return fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token",
			strings.TrimRight(keycloakURL, "/"), realm)
	}
	return ""
}

func refreshAccessToken(tokenURL, clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}
	return postToken(tokenURL, data)
}

func postToken(tokenURL string, data url.Values) (*TokenResponse, error) {
	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errBody struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Description != "" {
			return nil, fmt.Errorf("authentication failed: %s", errBody.Description)
		}
		return nil, fmt.Errorf("authentication failed: %s", resp.Status)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	return &tokenResp, nil
}

// AutoRefreshToken checks if the stored token is expired and refreshes it if possible.
func AutoRefreshToken(cfg *ConfigFile) (string, error) {
	if cfg.Token == "" {
		return "", nil
	}
	if cfg.ExpiresAt == "" {
		return cfg.Token, nil
	}

	expiresAt, err := time.Parse(time.RFC3339, cfg.ExpiresAt)
	if err != nil {
		return cfg.Token, nil
	}

	if time.Now().Before(expiresAt.Add(-30 * time.Second)) {
		return cfg.Token, nil
	}

	if cfg.RefreshToken == "" || cfg.ClientID == "" {
		return cfg.Token, nil
	}

	// Resolve token URL: use stored token-url, or fall back to Keycloak fields.
	tokenURL := cfg.TokenURL
	if tokenURL == "" && cfg.KeycloakURL != "" && cfg.Realm != "" {
		tokenURL = fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token",
			strings.TrimRight(cfg.KeycloakURL, "/"), cfg.Realm)
	}
	if tokenURL == "" {
		return cfg.Token, nil
	}

	tokenResp, err := refreshAccessToken(tokenURL, cfg.ClientID, cfg.ClientSecret, cfg.RefreshToken)
	if err != nil {
		return cfg.Token, nil
	}

	cfg.Token = tokenResp.AccessToken
	cfg.RefreshToken = tokenResp.RefreshToken
	cfg.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)
	SaveConfig(cfg)

	return cfg.Token, nil
}
