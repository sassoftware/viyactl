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

	"github.com/goccy/go-yaml"
	"github.com/sassoftware/viyactl/cmd"
	"github.com/sassoftware/viyactl/cmd/environment"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// Groups represents the Groups in a Viya 4 deployment
type Groups struct {
	Groups []Group `json:"groups,omitempty"`
}

// Group is one Group with it's members
type Group map[string]GroupDetail

// GroupDetail contains the name, description and members of a group
type GroupDetail struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
}

// viyaGroup is the representation of a group returned from SAS Viya
type viyaGroup struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
	ProviderID  string   `json:"providerId,omitempty"`
}

// Name returns a human readable name
func (*Groups) Name() string {
	return "groups"
}

// FileName returns the name of the yaml file associated with groups
func (*Groups) FileName() string {
	return "sitegroups.yaml"
}

// Read gets the caslibs from the given SAS Viya deployment
func (g *Groups) Read(token environment.Token, sasEndpoint string) error {
	var err error
	if cmd.FilterFile != "" {
		filter, err := getGroupFilterFromFile(cmd.FilterFile)
		if err != nil {
			return err
		}

		for _, group := range filter.Groups {
			var conditions []string
			for name, detail := range group {
				if detail.Name != "" {
					conditions = append(conditions, fmt.Sprintf("eq(id,'%s')", name))
				}
				if detail.Description != "" {
					conditions = append(conditions, fmt.Sprintf("eq(description,'%s')", detail.Description))
				}
				if len(conditions) > 1 {
					cmd.GroupFilters = append(cmd.GroupFilters, fmt.Sprintf("and('%s')", strings.Join(conditions, ",")))
				} else if len(conditions) == 1 {
					cmd.GroupFilters = append(cmd.GroupFilters, conditions[0])
				}
			}
		}
	}

	if strings.HasPrefix(sasEndpoint, "https://") {
		*g, err = getViyaGroups(token, sasEndpoint)
	} else {
		*g, err = getLocalGroups(sasEndpoint)
	}
	return err
}

// Write makes the groups on the SAS Viya environment the same as other, applying the minimum number of changes
func (g *Groups) Write(token environment.Token, sasEndpoint string, posterior any) error {
	otherGroup, ok := posterior.(*Groups)
	if !ok {
		return errors.New("other input to groups.Write must be a Group")
	}
	return overwriteGroups(token, sasEndpoint, g.Groups, otherGroup.Groups)
}

// YAML returns a YAML representation of groups
func (g *Groups) YAML() ([]byte, error) {
	b, err := yaml.MarshalWithOptions(g, yaml.IndentSequence(true), yaml.AutoInt())
	b = slices.Concat([]byte("---\n"), b)
	return b, err
}

// Filter takes a file, reads it and filters the struct inplace
func (g *Groups) Filter(filterPath string) error {
	filter, err := getGroupFilterFromFile(filterPath)
	if err != nil {
		return err
	}
	*g = filterGroups(*g, filter)
	return nil
}

func getGroupFilterFromFile(filterPath string) (Groups, error) {
	file, err := os.Open(filterPath)
	if err != nil {
		return Groups{}, fmt.Errorf("unable to open filter at %q", filterPath)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			zap.S().Warnw("unable to close file", "path", filterPath)
		}
	}()
	decoder := yaml.NewDecoder(file)

	var filter Groups
	for {
		var dc map[string]any
		if err := decoder.Decode(&dc); err != nil {
			break // EOF or error
		}

		var b []byte
		b, err = yaml.Marshal(dc)

		for k := range dc {
			if k == "groups" {
				multierr.AppendInto(&err, yaml.Unmarshal(b, &filter))
				break
			}
		}
	}

	return filter, err
}

// Clone creates an empty Groups object
func (*Groups) Clone() ConfigType {
	return &Groups{}
}

func init() {
	SupportedTypes = append(SupportedTypes, &Groups{})
}

// Reading

func collectPaginatedGroups(token environment.Token, sasEndpoint string, filter url.Values) ([]viyaGroup, error) {
	var items struct {
		Count      int         `json:"count"`
		ViyaGroups []viyaGroup `json:"items"`
	}

	baseURL := fmt.Sprintf("%s/identities/groups", sasEndpoint)

	filter.Set("start", strconv.Itoa(0))
	perform := baseURL + "?" + filter.Encode()

	req, _ := http.NewRequest("GET", perform, nil)
	req.Header.Add("Authorization", "BEARER "+token.AccessToken)
	req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
	req.Header.Add("Content-Type", "application/json")

	zap.S().Info("Getting first page of groups")
	resp, _ := cmd.Client.Do(req)

	a, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, cmd.ExitWithHTTPError(resp, "200", "https://developer.sas.com/rest-apis/identities/getGroups")
	}

	err := json.Unmarshal(a, &items)
	if err != nil {
		return items.ViyaGroups, fmt.Errorf("unable to unmarshal returned JSON: %q", string(a))
	}
	zap.S().Infow("Got total number of groups under filter", "total groups", len(items.ViyaGroups), "filter", filter)
	items.ViyaGroups = slices.Grow(items.ViyaGroups, items.Count-len(items.ViyaGroups))

	wg, _ := errgroup.WithContext(context.Background())

	for current := len(items.ViyaGroups); current < items.Count; current += 50 {
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

			a, _ := io.ReadAll(resp.Body)

			var items2 struct {
				ViyaGroups []viyaGroup `json:"items"`
			}

			err := json.Unmarshal(a, &items2)
			if err != nil {
				return fmt.Errorf("unable to unmarshal returned JSON: %q", string(a))
			}

			items.ViyaGroups = append(items.ViyaGroups, items2.ViyaGroups...)
			return nil
		})
	}

	err = wg.Wait()
	zap.S().Infow("Got all groups under filter", "filter", filter)
	return items.ViyaGroups, err
}

func getViyaGroups(token environment.Token, sasEndpoint string) (Groups, error) {
	zap.S().Info("Entered func(\"getViyaGroups\")")
	var viya Groups

	filter := url.Values{}
	filter.Set("providerId", "local")

	items, err := collectPaginatedGroups(token, sasEndpoint, filter)
	if err != nil {
		return Groups{}, err
	}
	zap.S().Info("Parsing groups to viyactl representation")

	for _, item := range items {
		if item.ProviderID != "local" {
			zap.S().Infow(cmd.Symbols["notify"]+"item already exists with provider, this will be shadowed", "item", item)
			continue
		}

		groupsURL, err := url.JoinPath(sasEndpoint, "/identities/groups", item.ID, "members/")
		if err != nil {
			return Groups{}, fmt.Errorf("unable to join %q %q %q %q into a valid url", sasEndpoint, "identities/groups", item.ID, "members")
		}
		req, _ := http.NewRequest("GET", groupsURL, nil)

		req.Header.Add("Authorization", "BEARER "+token.AccessToken)
		req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
		req.Header.Add("Content-Type", "application/json")

		resp, err := cmd.Client.Do(req)
		if err != nil {
			return Groups{}, fmt.Errorf("HTTP request errored")
		}

		b, _ := io.ReadAll(resp.Body)

		var remoteMembers struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}

		err = json.Unmarshal(b, &remoteMembers)
		if err != nil {
			return Groups{}, fmt.Errorf("unable to unmarshal returned JSON: %q", string(b))
		}

		memberIDs := make([]string, len(remoteMembers.Items))
		for i, m := range remoteMembers.Items {
			memberIDs[i] = m.ID
		}

		viya.Groups = append(viya.Groups, map[string]GroupDetail{item.ID: {
			Name:        item.Name,
			Description: item.Description,
			Members:     memberIDs,
		}})
	}

	if cmd.FilterFile != "" {
		err = viya.Filter(cmd.FilterFile)
	}

	return viya, err
}

func getLocalGroups(path string) (Groups, error) {
	zap.S().Info("Entered func(\"getLocalGroups\")")
	var local Groups
	buf, err := os.ReadFile(filepath.Join(path, local.FileName()))
	if err != nil {
		return local, fmt.Errorf("unable to read from %q", path)
	}

	err = yaml.Unmarshal([]byte(buf), &local)
	if err != nil {
		return local, fmt.Errorf("unable to unmarshal from %q", path)
	}

	if cmd.FilterFile != "" {
		err = local.Filter(cmd.FilterFile)
	}
	return local, err
}

func filterGroups(local, filter Groups) Groups {
	zap.S().Info("Entered func(\"filterGroups\")")
	newGroups := Groups{
		Groups: []Group{},
	}

	localGroupMap := make(map[string]GroupDetail)
	for _, group := range local.Groups {
		maps.Copy(localGroupMap, group)
	}

	for _, filterGroup := range filter.Groups {
		for groupName, filterDetail := range filterGroup {
			localDetail, exists := localGroupMap[groupName]
			if !exists {
				continue
			}

			var newMembers []string

			if len(filterDetail.Members) == 0 {
				newMembers = localDetail.Members
			} else {

				localMemberSet := make(map[string]struct{})
				for _, m := range localDetail.Members {
					localMemberSet[m] = struct{}{}
				}
				for _, m := range filterDetail.Members {
					if _, ok := localMemberSet[m]; ok {
						newMembers = append(newMembers, m)
					}
				}
			}

			newGroup := Group{
				groupName: GroupDetail{
					Name:        localDetail.Name,
					Description: localDetail.Description,
					Members:     newMembers,
				},
			}
			newGroups.Groups = append(newGroups.Groups, newGroup)
		}
	}

	return newGroups
}

// Writing

func overwriteGroups(token environment.Token, sasEndpoint string, viya, local []Group) error {
	viyaGroups := []string{}
	viyaMembers := map[string][]string{}
	for _, g := range viya {
		for id, detail := range g {
			viyaGroups = append(viyaGroups, id)
			viyaMembers[id] = append(viyaMembers[id], detail.Members...)
		}
	}

	// Create all groups
	for _, g := range local {
		for id, detail := range g {
			if !slices.Contains(viyaGroups, id) {
				groupsURL, err := url.JoinPath(sasEndpoint, "/identities/groups")
				if err != nil {
					return fmt.Errorf("could not join URLs: %s", err.Error())
				}

				group := struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					Description string `json:"description"`
				}{id, detail.Name, detail.Description}

				b, _ := json.Marshal(group)
				req, _ := http.NewRequest("POST", groupsURL, bytes.NewReader(b))
				req.Header.Add("Authorization", "BEARER "+token.AccessToken)
				req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
				req.Header.Add("Content-Type", "application/json")

				res, err := cmd.Client.Do(req)
				if err != nil || res.StatusCode != 201 {
					return cmd.ExitWithHTTPError(res, "201", "https://developer.sas.com/rest-apis/identities/createGroup#responses")
				}
			}
		}
	}

	// rm members TODO optimise
	for _, g := range viya {
		for id, detail := range g {
			var localGroupDetails GroupDetail
			// Only manage stuff in local for now
			if !slices.ContainsFunc(local, func(_ Group) bool {
				for _, gr := range local {
					if _, found := gr[id]; found {
						localGroupDetails = gr[id]
						return true
					}
				}
				return false
			}) {
				continue
			}

			for _, member := range detail.Members {
				if !slices.Contains(localGroupDetails.Members, member) {
					groupsURL, err := url.JoinPath(sasEndpoint, "identities/groups", id, "groupMembers", member)
					if err != nil {
						return fmt.Errorf("unable to join urls: '%s' '%s' '%s' '%s'", "identities/groups", id, "members", member)
					}
					req, _ := http.NewRequest("DELETE", groupsURL, nil)
					req.Header.Add("Authorization", "BEARER "+token.AccessToken)
					req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
					req.Header.Add("Content-Type", "application/json")

					res, err := cmd.Client.Do(req)
					if err != nil || res.StatusCode != 204 {
						return cmd.ExitWithHTTPError(res, "204", "https://developer.sas.com/rest-apis/identities/deleteGroupMember#responses")
					}
				}
			}
		}
	}

	// Init members TODO optimise
	for _, g := range local {
		for id, detail := range g {
			var viyaGroupDetails GroupDetail
			for _, gr := range viya {
				if _, found := gr[id]; found {
					viyaGroupDetails = gr[id]
				}
			}

			for _, member := range detail.Members {
				if !slices.Contains(viyaGroupDetails.Members, member) {
					groupsURL, err := url.JoinPath(sasEndpoint, "identities/groups", id, "groupMembers", member)
					if err != nil {
						return fmt.Errorf("unable to join urls: '%s' '%s' '%s' '%s'", "identities/groups", id, "members", member)
					}
					req, _ := http.NewRequest("PUT", groupsURL, nil)
					req.Header.Add("Authorization", "BEARER "+token.AccessToken)
					req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
					req.Header.Add("Content-Type", "application/json")

					res, err := cmd.Client.Do(req)
					if err != nil || res.StatusCode != 201 {
						return cmd.ExitWithHTTPError(res, "201", "https://developer.sas.com/rest-apis/identities/updateGroupMember#responses")
					}
				}
			}
		}
	}

	zap.S().Infow("Finished writing groups", "sasEndpoint", sasEndpoint)
	return nil
}
