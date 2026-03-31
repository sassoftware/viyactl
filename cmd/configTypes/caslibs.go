// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package configtypes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
	"github.com/sassoftware/viyactl/cmd"
	"github.com/sassoftware/viyactl/cmd/environment"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// Caslibs represent the Caslibs of a Viya 4 deployment
type Caslibs struct {
	Caslibs map[string]Caslib `json:"caslibs"`
}

// Caslib represents an individual Caslib
type Caslib struct {
	Type        string               `json:"type"`
	Path        string               `json:"path,omitempty"`
	Scope       string               `json:"scope,omitempty"`
	Attributes  map[string]any       `json:"attributes"`
	Description string               `json:"description"`
	CasServer   string               `json:"cas_server"`
	Permissions CaslibPrincipalTypes `json:"permissions"`
}

// CaslibPrincipalTypes represents the Principal types associated with a Caslib
// Caslibs support different permissions thus this is necessary
type CaslibPrincipalTypes struct {
	PrincipalType map[string]CaslibPrincipals `yaml:",inline" validate:"dive,keys,supportedPrincipalTypes,endkeys"`
}

// CaslibPrincipals represents the principals associated with a principal type
type CaslibPrincipals struct {
	Principal map[string]CaslibPermissions `yaml:",inline"`
}

// CaslibPermissions represents the list of rules that are granted, TODO link to docs
type CaslibPermissions struct {
	RuleType map[string][]string `yaml:",inline" validate:"dive,keys,supportedCaslibRuleTypes,endkeys,dive,supportedCaslibPermissions"`
}

// Name returns a human readable name
func (*Caslibs) Name() string {
	return "caslibs"
}

// FileName returns the name of the yaml file associated with caslibs
func (*Caslibs) FileName() string {
	return "sitecaslibs.yaml"
}

// Read gets the caslibs from the given SAS Viya deployment
func (c *Caslibs) Read(token environment.Token, sasEndpoint string) error {
	var err error

	if cmd.FilterFile != "" {
		filter, err := getCaslibFilterFromFile(cmd.FilterFile)
		if err != nil {
			return err
		}
		// https://developer.sas.com/rest-apis/casManagement/getCaslibs#query-parameters
		for k := range filter.Caslibs {
			cmd.CaslibFilters = append(cmd.CaslibFilters, fmt.Sprintf("eq(name,'%s')", k))
		}
	}
	if strings.HasPrefix(sasEndpoint, "https://") {
		*c, err = getViyaCaslibs(token, sasEndpoint)
	} else {
		*c, err = getLocalCaslibs(sasEndpoint)
	}
	return err
}

// Write makes the caslibs on the SAS Viya environment the same as other, applying the minimum number of changes
func (c *Caslibs) Write(token environment.Token, sasEndpoint string, other any) error {
	otherCaslibs, ok := other.(*Caslibs)
	if !ok {
		return errors.New("other input to caslibs.Write must be a Caslibn")
	}
	return caslibs(token, sasEndpoint, *c, *otherCaslibs)
}

// YAML returns a YAML representation of caslibs
func (c *Caslibs) YAML() ([]byte, error) {
	b, err := yaml.MarshalWithOptions(c, yaml.IndentSequence(true), yaml.AutoInt())
	b = slices.Concat([]byte("---\n"), b)
	return b, err
}

func getCaslibFilterFromFile(path string) (Caslibs, error) {
	file, err := os.Open(path)
	if err != nil {
		return Caslibs{}, fmt.Errorf("unable to open filter at %q", path)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			zap.S().Warnw("unable to close file", "path", path)
		}
	}()
	decoder := yaml.NewDecoder(file)

	var filter Caslibs
	for {
		var dc map[string]any
		if err := decoder.Decode(&dc); err != nil {
			break // EOF or error
		}

		var b []byte
		b, err = yaml.Marshal(dc)

		for k := range dc {
			if k == "caslibs" {
				multierr.AppendInto(&err, yaml.Unmarshal(b, &filter))
				break
			}
		}
	}

	return filter, err
}

// Filter takes a file, reads it and filters the struct inplace
func (c *Caslibs) Filter(filterPath string) error {
	filter, err := getCaslibFilterFromFile(filterPath)
	*c = filterCaslibs(*c, filter)
	return err
}

// Clone creates an empty Caslibs object
func (*Caslibs) Clone() ConfigType {
	return &Caslibs{}
}

func init() {
	SupportedTypes = append(SupportedTypes, &Caslibs{})
}

// Reading

type viyaCaslib struct {
	Name        string `json:"name"`
	Path        string
	Type        string
	Description string
	Attributes  map[string]any
}

func collectPaginatedCaslibs(token environment.Token, sasEndpoint string, filter url.Values) ([]viyaCaslib, error) {
	var items struct {
		Count       int          `json:"count"`
		ViyaCaslibs []viyaCaslib `json:"items"`
	}

	baseURL := fmt.Sprintf("%s/casManagement/servers/cas-shared-default/caslibs", sasEndpoint)

	filter.Set("start", strconv.Itoa(0))
	filter.Set("limit", "50")
	perform := baseURL + "?" + filter.Encode()

	req, _ := http.NewRequest("GET", perform, nil)
	req.Header.Add("Authorization", "BEARER "+token.AccessToken)
	req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
	req.Header.Add("Content-Type", "application/json")

	res, _ := cmd.Client.Do(req)

	a, _ := io.ReadAll(res.Body)

	err := json.Unmarshal(a, &items)
	if err != nil {
		return nil, cmd.ExitWithHTTPError(res, "200", "https://developer.sas.com/rest-apis/casManagement/getCaslibs#responses")
	}
	items.ViyaCaslibs = slices.Grow(items.ViyaCaslibs, items.Count-len(items.ViyaCaslibs))

	wg, _ := errgroup.WithContext(context.Background())

	for current := len(items.ViyaCaslibs); current < items.Count; current += 50 {
		wg.Go(func() error {
			filter2 := url.Values{}
			maps.Copy(filter2, filter)
			filter2.Set("start", strconv.Itoa(current))
			perform := baseURL + "?" + filter2.Encode()

			req, _ := http.NewRequest("GET", perform, nil)
			req.Header.Add("Authorization", "BEARER "+token.AccessToken)
			req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
			req.Header.Add("Content-Type", "application/json")

			resp, _ := cmd.Client.Do(req)

			a, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != 200 {
				return cmd.ExitWithHTTPError(resp, "200", "https://developer.sas.com/rest-apis/casManagement/getCaslibs#responses")
			}

			var items2 struct {
				ViyaCaslibs []viyaCaslib `json:"items"`
			}

			err = json.Unmarshal(a, &items2)
			if err != nil {
				return cmd.ExitWithHTTPError(resp, "200", "https://developer.sas.com/rest-apis/casManagement/getCaslibs#responses")
			}

			items.ViyaCaslibs = append(items.ViyaCaslibs, items2.ViyaCaslibs...)
			return nil
		})
	}

	err = wg.Wait()
	return items.ViyaCaslibs, err
}

func getViyaCaslibs(token environment.Token, sasEndpoint string) (Caslibs, error) {
	filter := url.Values{}
	if len(cmd.CaslibFilters) > 1 {
		filter.Set("filter", fmt.Sprintf("or(%s)", strings.Join(cmd.CaslibFilters, ",")))
	} else if len(cmd.CaslibFilters) == 1 {
		filter.Set("filter", cmd.CaslibFilters[0])
	}

	items, err := collectPaginatedCaslibs(token, sasEndpoint, filter)
	if err != nil {
		return Caslibs{}, err
	}

	wg, _ := errgroup.WithContext(context.Background())

	viya := map[string]Caslib{}
	var mu sync.Mutex

	for _, cas := range items {
		wg.Go(func() error {
			var ctems struct {
				Items []struct {
					PrincipalType string `json:"identityType"`
					Type          string
					Permission    string
					Principal     string `json:"identity"`
				} `json:"items"`
			}

			baseURL := fmt.Sprintf("%s/casAccessManagement/servers/cas-shared-default/caslibControls/%s", sasEndpoint, cas.Name)

			filter = url.Values{}
			filter.Set("start", strconv.Itoa(0))
			perform := baseURL + "?" + filter.Encode()

			req, _ := http.NewRequest("GET", perform, nil)
			req.Header.Add("Authorization", "BEARER "+token.AccessToken)
			req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
			req.Header.Add("Content-Type", "application/json")

			resp, _ := cmd.Client.Do(req)

			a, _ := io.ReadAll(resp.Body)

			err := json.Unmarshal(a, &ctems)
			if err != nil {
				zap.S().Infow("Unable to unmarshal returned JSON", "JSON", string(a))
				return fmt.Errorf("TODO(contact viyactl support) unable to unmarshal returned JSON: %q", string(a))
			}

			c := Caslib{
				Type:        cas.Type,
				Path:        cas.Path,
				Description: cas.Description,
				CasServer:   "cas-shared-default",
				Permissions: CaslibPrincipalTypes{PrincipalType: map[string]CaslibPrincipals{}},
				Attributes:  cas.Attributes,
			}

			for _, item := range ctems.Items {
				// ensure principal type bucket exists
				principals, ok := c.Permissions.PrincipalType[item.PrincipalType]
				if !ok {
					principals = CaslibPrincipals{
						Principal: make(map[string]CaslibPermissions),
					}
				}
				if principals.Principal == nil {
					principals.Principal = make(map[string]CaslibPermissions)
				}

				newPermissions := CaslibPermissions{
					RuleType: map[string][]string{
						item.Type: {item.Permission},
					},
				}

				if existingPerms, ok := principals.Principal[item.Principal]; ok {
					// recursive merge: merge map keys, append slice values
					mergeCaslibPermissions(&existingPerms, newPermissions)
					principals.Principal[item.Principal] = existingPerms
				} else {
					principals.Principal[item.Principal] = newPermissions
				}

				c.Permissions.PrincipalType[item.PrincipalType] = principals
			}

			mu.Lock()
			viya[cas.Name] = c
			mu.Unlock()
			return nil
		})
	}

	if err = wg.Wait(); err != nil {
		return Caslibs{}, err
	}

	v := Caslibs{viya}

	if cmd.FilterFile != "" {
		err = v.Filter(cmd.FilterFile)
	}

	return v, err
}

func mergeRuleType(dst, src map[string][]string) map[string][]string {
	if dst == nil {
		dst = make(map[string][]string, len(src))
	}
	for k, v := range src {
		dst[k] = append(dst[k], v...)
	}
	return dst
}

func mergeCaslibPermissions(dst *CaslibPermissions, src CaslibPermissions) {
	dst.RuleType = mergeRuleType(dst.RuleType, src.RuleType)
}

func getLocalCaslibs(path string) (Caslibs, error) {
	var local Caslibs
	buf, err := os.ReadFile(filepath.Join(path, local.FileName()))
	if err != nil {
		return local, fmt.Errorf("unable to read from %q", path)
	}

	if cmd.FilterFile != "" {
		err = local.Filter(cmd.FilterFile)
	}
	if err != nil {
		return local, err
	}

	err = yaml.Unmarshal([]byte(buf), &local)
	return local, err
}

func filterCaslibs(local, filter Caslibs) Caslibs {
	newCaslibs := Caslibs{
		Caslibs: make(map[string]Caslib),
	}

	for caslibName, filterCaslib := range filter.Caslibs {
		localCaslib, ok := local.Caslibs[caslibName]
		if !ok {
			continue // Only include caslibs that exist in both local and filter
		}

		newPermissions := CaslibPrincipalTypes{
			PrincipalType: make(map[string]CaslibPrincipals),
		}

		for principalType, filterPrincipals := range filterCaslib.Permissions.PrincipalType {
			// If filter.Caslibs[caslibName].Permissions.PrincipalType[principalType] is empty, keep all from local
			if len(filterPrincipals.Principal) == 0 {
				if localPrincipalMap, ok := localCaslib.Permissions.PrincipalType[principalType]; ok {
					newPermissions.PrincipalType[principalType] = localPrincipalMap
				}
				continue
			}

			for principal := range filterPrincipals.Principal {
				if localPrincipalMap, ok := localCaslib.Permissions.PrincipalType[principalType]; ok {
					if localPermissions, ok := localPrincipalMap.Principal[principal]; ok {
						if _, exists := newPermissions.PrincipalType[principalType]; !exists {
							newPermissions.PrincipalType[principalType] = CaslibPrincipals{
								Principal: make(map[string]CaslibPermissions),
							}
						}
						newPermissions.PrincipalType[principalType].Principal[principal] = localPermissions
					}
				}
			}
		}

		// If filter permissions are completely empty, copy all from local
		if len(filterCaslib.Permissions.PrincipalType) == 0 {
			newCaslibs.Caslibs[caslibName] = localCaslib
			continue
		}

		// Only add caslib if we have at least one principalType after filtering
		if len(newPermissions.PrincipalType) > 0 {
			newCaslib := localCaslib
			newCaslib.Permissions = newPermissions
			newCaslibs.Caslibs[caslibName] = newCaslib
		}
	}

	return newCaslibs
}

// Writing

type caslibPatch struct {
	Version      int    `json:"version"`
	Type         string `json:"type"`
	Permission   string `json:"permission"`
	IdentityType string `json:"identityType"`
	Identity     string `json:"identity"`
}

func permissionsToPatches(principals map[string]CaslibPrincipals) []caslibPatch {
	patches := []caslibPatch{}
	for identityType, permit := range principals {
		for identity, permissions := range permit.Principal {
			for typ, permission := range permissions.RuleType {
				for _, p := range permission {
					patch := caslibPatch{
						Version:      1,
						Type:         typ,
						Permission:   p,
						IdentityType: identityType,
						Identity:     identity,
					}
					patches = append(patches, patch)
				}
			}
		}
	}
	return patches
}

func caslibs(token environment.Token, sasEndpoint string, viya, local Caslibs) error {
	viyaKeys := slices.Collect(maps.Keys(viya.Caslibs))

	// TODO: create a session - "Passing sessionId is provided to avoid creating a new CAS session, improving performance of the call."

	for name, caslib := range local.Caslibs {
		if !slices.Contains(viyaKeys, name) {
			createURL := fmt.Sprintf("%s/casManagement/servers/%s/caslibs", sasEndpoint, caslib.CasServer)

			cas := struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Type        string         `json:"type"`
				Path        string         `json:"path"`
				Attributes  map[string]any `json:"attributes"`
			}{
				Name:        name,
				Description: caslib.Description,
				Type:        caslib.Type,
				Path:        caslib.Path,
				Attributes:  caslib.Attributes,
			}

			b, err := json.Marshal(cas)
			if err != nil {
				return fmt.Errorf("TODO(contact viyactl support) unable to marshal patch to json: %s", err.Error())
			}

			req, _ := http.NewRequest("POST", createURL, bytes.NewReader(b))

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("Accept", "application/json, application/vnd.sas.collection+json")
			// Log before auth added
			req.Header.Add("Authorization", "Bearer "+token.AccessToken)

			res, _ := cmd.Client.Do(req)
			if res.StatusCode != 201 && res.StatusCode != 200 {
				return cmd.ExitWithHTTPError(res, "201", "https://developer.sas.com/rest-apis/casManagement/createCaslib#responses")
			}
			err = patchPermissions(caslib.Permissions.PrincipalType, sasEndpoint, caslib.CasServer, name, token)
			if err != nil {
				return err
			}
		}
	}

	zap.S().Infow("Finished writing caslibs", "sasEndpoint", sasEndpoint)
	return nil
}

func patchPermissions(permissions map[string]CaslibPrincipals, sasEndpoint, casServer, name string, token environment.Token) error {
	patches := permissionsToPatches(permissions)

	patchURL := fmt.Sprintf("%s/casAccessManagement/servers/%s/caslibControls/%s", sasEndpoint, casServer, name)
	b, err := json.Marshal(patches)
	if err != nil {
		return fmt.Errorf("TODO(contact viyactl support) unable to marshal patch to json: %s", err.Error())
	}

	req, _ := http.NewRequest("PUT", patchURL, bytes.NewReader(b))

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json, application/vnd.sas.collection+json")
	// Log before auth added
	req.Header.Add("Authorization", "Bearer "+token.AccessToken)

	res, _ := cmd.Client.Do(req)
	if res.StatusCode != 201 && res.StatusCode != 200 {
		return cmd.ExitWithHTTPError(res, "201", "https://developer.sas.com/rest-apis/casManagement/createCaslib")
	}
	return nil
}
