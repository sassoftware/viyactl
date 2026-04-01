// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
// Package environment contains reusable functionality used by other packages
package environment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sassoftware/viyactl/cmd"
	"go.uber.org/zap"
)

// Token is an authorisation token: https://communities.sas.com/t5/SAS-Communities-Library/Go-Viya-First-steps-with-Go-language-and-SAS-Viya/ta-p/704659
type Token struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
	Jti         string `json:"jti"`
}

// ErrTimedOut is produced when a context times out
var ErrTimedOut = errors.New("timeout exceeded")

// Authenticate creates a token
func Authenticate(ctx context.Context, sasEndpoint string) (Token, error) {
	if !strings.HasPrefix(sasEndpoint, "https://") {
		return Token{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, cmd.Timeout)
	defer cancel()

	credentials, found := cmd.Auth[sasEndpoint]
	if !found {
		credentials = cmd.Auth[""] // Default credentials
	}

	var token Token

	var envAuth cmd.AuthInfo

	params := url.Values{}
	if credentials.ClientID != "" && credentials.ClientSecret != "" {
		params.Add("grant_type", "client_credentials")

		zap.S().Infow("Using client ID and client secret", "SAS endpoint", sasEndpoint)
	} else if credentials.Username != "" && credentials.Password != "" {
		params.Add("grant_type", "password")
		params.Add("username", credentials.Username)
		params.Add("password", credentials.Password)

		zap.S().Infow("Using username and password", "SAS endpoint", sasEndpoint)
	} else {
		var err error
		envAuth, err = cmd.GetCredentials(sasEndpoint)
		if err != nil {
			return token, err
		}

		if envAuth.ClientID != "" && envAuth.ClientSecret != "" {
			params.Add("grant_type", "client_credentials")
			zap.S().Infow("Using client ID and client secret from environment", "SAS endpoint", sasEndpoint)
		} else if envAuth.Username != "" && envAuth.Password != "" {
			params.Add("grant_type", "password")
			params.Add("username", envAuth.Username)
			params.Add("password", envAuth.Password)
			zap.S().Infow("Using username and password from environment", "SAS endpoint", sasEndpoint)
		} else {
			return token, fmt.Errorf("unable to find credentials for %s, please set either environment variables, or use an authinfo file", sasEndpoint)
		}
	}

	body := strings.NewReader(params.Encode())

	authURL, err := url.JoinPath(sasEndpoint, "SASLogon/oauth/token")
	if err != nil {
		return token, fmt.Errorf("unable to join %q and %q into a valid url", sasEndpoint, "SASLogon/oauth/token")
	}
	zap.S().Infow("Created auth URL", "authURL", authURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authURL, body)
	if err != nil {
		return token, fmt.Errorf("unable to create http request")
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("sas.cli", "")

	if credentials.ClientID != "" && credentials.ClientSecret != "" {
		req.SetBasicAuth(credentials.ClientID, credentials.ClientSecret)
	} else if envAuth.ClientID != "" && envAuth.ClientSecret != "" {
		req.SetBasicAuth(envAuth.ClientID, envAuth.ClientSecret)
	}

	zap.S().Infow("Authenticating", "SAS endpoint", sasEndpoint)
	resp, err := cmd.Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return token, ErrTimedOut
		}
		return token, err
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			zap.S().Warn("Unable to close response body after authentication", "sasEndpoint", sasEndpoint)
		}
	}()

	zap.S().Infow("Received response", "SAS endpoint", sasEndpoint, "StatusCode", resp.StatusCode)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		j := struct {
			Message string `json:"message"`
			Details any    `json:"details"`
		}{}
		_ = json.Unmarshal(b, &j)
		return token, fmt.Errorf("expected %d got %d, reference https://developer.sas.com/rest-apis/SASLogon-v1/grantClientCredentials#responses\nmessage:%s\ndetails:%q", 200, resp.StatusCode, j.Message, j.Details)
	}

	bodyReader, err := io.ReadAll(resp.Body)
	if err != nil {
		return token, fmt.Errorf("unable to read returned token, got %q", resp.Body)
	}
	err = json.Unmarshal(bodyReader, &token)
	if err != nil {
		return token, fmt.Errorf("unable to unmarshal %q", string(bodyReader))
	}

	return token, nil
}
