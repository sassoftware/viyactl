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

// Folders represents the folder structure of a Viya 4 deployment
type Folders struct {
	Folders []Folder `json:"folders,omitempty"`
}

// Folder is an individual folder with the controls
type Folder struct {
	Name        string                      `json:"name"`
	Path        string                      `json:"path"`
	ID          string                      `json:"id"`
	Parent      string                      `json:"parent"`
	Description string                      `json:"description"`
	Controls    map[string]FolderPrincipals `json:"controls,omitempty"`
}

// FolderPrincipals is equivalent to Principals, but only allows one as we can't have multiple conditions
type FolderPrincipals struct {
	Principal map[string]Properties `yaml:",inline" validate:"required"` // Principal to the Rule types
}

// MarshalYAML defines how a struct gets written to a YAML string
//
// Folder ignores the parent and only writes the Path, Description and Controls
func (f Folder) MarshalYAML() ([]byte, error) {
	type Alias struct {
		Path        string                      `json:"path"`
		Description string                      `json:"description"`
		Controls    map[string]FolderPrincipals `json:"controls,omitempty"`
	}
	return yaml.Marshal(Alias{
		Path:        f.Path,
		Description: f.Description,
		Controls:    f.Controls,
	})
}

// Name returns a human readable name
func (*Folders) Name() string {
	return "folders"
}

// FileName returns the name of the yaml file associated with folders
func (*Folders) FileName() string {
	return "sitefolders.yaml"
}

// Read gets the folders from the given SAS Viya deployment
func (f *Folders) Read(token environment.Token, sasEndpoint string) error {
	var err error

	if cmd.FilterFile != "" {
		filter, err := getFolderFilterFromFile(cmd.FilterFile)
		if err != nil {
			return err
		}
		for _, folder := range filter.Folders {
			var conditions []string
			if folder.Path != "" {
				lastPart := strings.LastIndex(folder.Path, "/")
				if lastPart != -1 {
					conditions = append(conditions, fmt.Sprintf("contains(name,'%s')", folder.Path[lastPart+1:]))
					// Need to add parent filter
					conditions = append(conditions, fmt.Sprintf("eq(parent,'%s')", folder.Path[:lastPart]))
				} else {
					conditions = append(conditions, fmt.Sprintf("contains(name,'%s')", folder.Path))
				}
			}
			if folder.Description != "" {
				conditions = append(conditions, fmt.Sprintf("eq(description,'%s')", folder.Description))
			}
			if len(conditions) > 1 {
				cmd.FolderFilters = append(cmd.FolderFilters, fmt.Sprintf("and(%s)", strings.Join(conditions, ",")))
			} else {
				cmd.FolderFilters = append(cmd.FolderFilters, conditions[0])
			}
		}
	}
	if strings.HasPrefix(sasEndpoint, "https://") {
		*f, err = getViyaFolders(token, sasEndpoint)
	} else {
		*f, err = getLocalFolders(sasEndpoint)
	}
	return err
}

// Write makes the folders on the SAS Viya environment the same as other, applying the minimum number of changes
func (f *Folders) Write(token environment.Token, sasEndpoint string, other any) error {
	otherFolders, ok := other.(*Folders)
	if !ok {
		return errors.New("other input to folders.Write must be a Folder")
	}
	return overwriteFolders(token, sasEndpoint, *f, *otherFolders)
}

// YAML returns a YAML representation of folders
func (f *Folders) YAML() ([]byte, error) {
	b, err := yaml.MarshalWithOptions(f, yaml.IndentSequence(true), yaml.AutoInt())
	b = slices.Concat([]byte("---\n"), b)
	return b, err
}

func getFolderFilterFromFile(filterPath string) (Folders, error) {
	file, err := os.Open(filterPath)
	if err != nil {
		return Folders{}, fmt.Errorf("unable to open filter at %q", filterPath)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			zap.S().Warnw("unable to close file", "path", filterPath)
		}
	}()
	decoder := yaml.NewDecoder(file)

	var filter Folders
	for {
		var dc map[string]any
		if err := decoder.Decode(&dc); err != nil {
			break // EOF or error
		}

		var b []byte
		b, err = yaml.Marshal(dc)

		for k := range dc {
			if k == "folders" {
				multierr.AppendInto(&err, yaml.Unmarshal(b, &filter))
				break
			}
		}
	}

	return filter, err
}

// Filter takes a file, reads it and filters the struct inplace
func (f *Folders) Filter(filterPath string) error {
	filter, err := getFolderFilterFromFile(filterPath)
	*f = filterFolders(*f, filter)
	return err
}

// Clone creates an empty folders object
func (*Folders) Clone() ConfigType {
	return &Folders{}
}

func init() {
	SupportedTypes = append(SupportedTypes, &Folders{})
}

// Reading

func resolveItemName(currentItem folderItem, items []folderItem) string {
	if currentItem.ParentFolderURI == "" {
		return currentItem.Name
	}

	target, _ := strings.CutPrefix(currentItem.ParentFolderURI, "/folders/folders/")
	index := slices.IndexFunc(items, func(it folderItem) bool {
		return it.ID == target
	})
	if index == -1 {
		return currentItem.Name // This is *UNDENIABLY* a hack, but can't do better with current REST API
	}

	return resolveItemName(items[index], items) + "/" + currentItem.Name
}

type folderItem struct {
	Name            string `json:"name"`
	MemberCount     int    `json:"memberCount"`
	Description     string `json:"description"`
	ID              string `json:"id"`
	ParentFolderURI string `json:"parentFolderUri"`
}

func collectPaginatedFolders(token environment.Token, sasEndpoint string, filter url.Values) ([]folderItem, error) {
	var items struct {
		Count       int          `json:"count"`
		ViyaFolders []folderItem `json:"items"`
	}

	baseURL := fmt.Sprintf("%s/folders/folders", sasEndpoint)

	filter.Set("start", strconv.Itoa(0))
	filter.Set("limit", "50")
	perform := baseURL + "?" + filter.Encode()

	req, _ := http.NewRequest("GET", perform, nil)
	req.Header.Add("Authorization", "BEARER "+token.AccessToken)
	req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
	req.Header.Add("Content-Type", "application/json")

	resp, _ := cmd.Client.Do(req)

	a, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, cmd.ExitWithHTTPError(resp, "200", "https://developer.sas.com/rest-apis/folders/folders")
	}

	err := json.Unmarshal(a, &items)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal returned JSON: %q", string(a))
	}
	items.ViyaFolders = slices.Grow(items.ViyaFolders, items.Count-len(items.ViyaFolders))

	wg, _ := errgroup.WithContext(context.Background())

	for current := len(items.ViyaFolders); current < items.Count; current += 50 {
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
				ViyaFolders []folderItem `json:"items"`
			}
			err := json.Unmarshal(a, &items2)
			if err != nil {
				return fmt.Errorf("unable to unmarshal returned JSON: %q", string(a))
			}

			items.ViyaFolders = append(items.ViyaFolders, items2.ViyaFolders...)
			return nil
		})
	}

	err = wg.Wait()
	return items.ViyaFolders, err
}

func getViyaFolders(token environment.Token, sasEndpoint string) (Folders, error) {
	var viya Folders

	folderFilter := url.Values{}
	if len(cmd.FolderFilters) > 1 {
		folderFilter.Set("filter", fmt.Sprintf("or(%s)", strings.Join(cmd.FolderFilters, ",")))
	}
	if len(cmd.FolderFilters) == 1 {
		folderFilter.Set("filter", cmd.FolderFilters[0])
	}

	items, err := collectPaginatedFolders(token, sasEndpoint, folderFilter)
	if err != nil {
		return viya, err
	}

	filter := url.Values{}

	filter.Set("filter", fmt.Sprintf("contains(objectUri,'%s')", "/folders/"))

	folderRules, err := collectPaginatedRules(token, sasEndpoint, filter)
	if err != nil {
		return viya, err
	}

	for _, item := range items {
		index := slices.IndexFunc(folderRules, func(i viyaRule) bool {
			return strings.Contains(i.ObjectURI, item.ID)
		})

		if index != -1 {

			individualPermit := folderRules[index]
			for i, p := range individualPermit.Permissions {
				individualPermit.Permissions[i] = strings.ToLower(p)
			}

			viya.Folders = append(viya.Folders, Folder{
				Path:        resolveItemName(item, items),
				ID:          item.ID,
				Description: item.Description,
				Parent:      item.ParentFolderURI,
				Controls: map[string]FolderPrincipals{
					individualPermit.PrincipalType: {
						Principal: map[string]Properties{
							individualPermit.Principal: {
								Reason:      individualPermit.Reason,
								Description: individualPermit.Description,
								// ID:          individualPermit.ID,
								Permissions: Permissions{
									RuleType: map[string][]string{
										individualPermit.Type: individualPermit.Permissions,
										// individualPermit.Type: append(individualPermit.Permissions, "ID:"+individualPermit.ID),
									},
								},
							},
						},
					},
				},
			})
		} else {
			viya.Folders = append(viya.Folders, Folder{
				Path:        resolveItemName(item, items),
				ID:          item.ID,
				Description: item.Description,
				Parent:      item.ParentFolderURI,
				Controls:    nil,
			})
		}
	}
	if cmd.FilterFile != "" {
		err = viya.Filter(cmd.FilterFile)
	}

	return viya, err
}

func getLocalFolders(path string) (Folders, error) {
	var local Folders
	buf, err := os.ReadFile(filepath.Join(path, local.FileName()))
	if err != nil {
		return local, fmt.Errorf("unable to read from %q", path)
	}
	err = yaml.Unmarshal([]byte(buf), &local)
	if err != nil {
		return local, err
	}

	if cmd.FilterFile != "" {
		err = local.Filter(cmd.FilterFile)
	}

	return local, err
}

func filterFolders(local, filter Folders) Folders {
	newFolders := Folders{
		Folders: []Folder{},
	}

	// Create a lookup map for local folders by path for quick access
	localFolderMap := make(map[string]Folder)
	for _, folder := range local.Folders {
		localFolderMap[folder.Path] = folder
	}
	for _, filterFolder := range filter.Folders {
		localFolder, ok := localFolderMap[filterFolder.Path]
		if !ok {
			continue // Only include folders that exist in both local and filter
		}

		newControls := make(map[string]FolderPrincipals)

		for principalType, filterPrincipals := range filterFolder.Controls {
			// If filterFolder.Controls[principalType] is empty, keep all from local
			if len(filterPrincipals.Principal) == 0 {
				if localPrincipals, ok := localFolder.Controls[principalType]; ok {
					newControls[principalType] = localPrincipals
				}
				continue
			}

			for principal := range filterPrincipals.Principal {
				if localPrincipals, ok := localFolder.Controls[principalType]; ok {
					if localProperties, ok := localPrincipals.Principal[principal]; ok {
						if _, exists := newControls[principalType]; !exists {
							newControls[principalType] = FolderPrincipals{
								Principal: make(map[string]Properties),
							}
						}
						newControls[principalType].Principal[principal] = localProperties
					}
				}
			}
		}

		// If filter controls are completely empty, copy all from local
		if len(filterFolder.Controls) == 0 {
			newFolders.Folders = append(newFolders.Folders, localFolder)
			continue
		}

		newFolder := localFolder
		newFolder.Controls = newControls
		newFolders.Folders = append(newFolders.Folders, newFolder)
	}

	return newFolders
}

// Writing

var (
	cache     = map[string]string{}
	cacheKeys = []string{}
)

// createFolder creates all of the parents of the wanted path that do not already exist, then creates the path
func createFolder(path, description, sasEndpoint string, token environment.Token, viya []Folder) (string, error) {
	parent := ""
	child := path
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash != -1 {
		parent = path[:lastSlash]
		child = path[lastSlash+1:]
	}
	viyaIndex := slices.IndexFunc(viya, func(viyafolder Folder) bool {
		return parent == viyafolder.Path
	})

	// Make parent if it does not exist
	if viyaIndex == -1 && !slices.Contains(cacheKeys, parent) && parent != "" {
		_, err := createFolder(parent, description, sasEndpoint, token, viya)
		if err != nil {
			return "", err
		}
	}

	// Parent is now guaranteed to either be in viya or the cache (or be root)
	parentID := "none"
	if viyaIndex != -1 {
		parentID = viya[viyaIndex].ID
	}
	if slices.Contains(cacheKeys, parent) {
		parentID = cache[parent]
	}

	// create the folder with necessary parent
	createURL := fmt.Sprintf("%s/folders/folders?parentFolderUri=%s", sasEndpoint, parentID)
	if parentID != "none" {
		createURL = fmt.Sprintf("%s/folders/folders?parentFolderUri=/folders/folders/%s", sasEndpoint, parentID)
	}

	if description == "" {
		description = "Folder created by Viyactl"
	}
	body := struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}{
		Name:        child,
		Description: description,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("unable to unmarshal response: %s", err.Error())
	}

	req, _ := http.NewRequest("POST", createURL, bytes.NewReader(b))
	req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "BEARER "+token.AccessToken)

	res, _ := cmd.Client.Do(req)
	if res.StatusCode != 201 {
		return "", cmd.ExitWithHTTPError(res, "201", "https://developer.sas.com/rest-apis/folders/createFolder#responses")
	}
	a, _ := io.ReadAll(res.Body)
	ret := struct {
		ID string `json:"id"`
	}{}
	err = json.Unmarshal(a, &ret)
	if err != nil {
		return "", fmt.Errorf("unable to unmarshal response: %s", err.Error())
	}

	cache[path] = ret.ID
	cacheKeys = append(cacheKeys, path)
	return ret.ID, nil
}

func overwriteFolders(token environment.Token, sasEndpoint string, viyaEnv, localEnv Folders) error {
	viya := viyaEnv.Folders
	local := localEnv.Folders

	var patches []Patch

	var newFolders []Folder

	var count int

	for _, f := range local {
		// 'You cannot create an item in the Recycle Bin folder'
		if _, after, found := strings.Cut(f.Path, "/Recycle Bin/"); found && len(after) > 0 {
			zap.S().Infow("You cannot create an item in the Recycle Bin folder, skipping...", "Path", f.Path)
			continue
		}

		// If in local but not Viya create
		viyaIndex := slices.IndexFunc(viya, func(viyafolder Folder) bool {
			return f.Path == viyafolder.Path
		})

		if viyaIndex == -1 {
			newFolders = append(newFolders, f)
			continue
		}

		// Check if description is correct
		if viya[viyaIndex].Description != f.Description {
			parts := strings.Split(f.Path, "/")

			_, parent, found := strings.Cut(viya[viyaIndex].Parent, "/folders/folders/")

			fmt.Printf("found:%v - parent:%s", found, parent)
			payload := struct {
				Name            string `json:"name"`
				Description     string `json:"description"`
				ParentFolderURI string `json:"parentFolderUri"`
			}{
				Name:            parts[len(parts)-1],
				Description:     f.Description,
				ParentFolderURI: parent,
			}

			b, err := json.Marshal(payload)
			if err != nil {
				return err
			}

			a, _ := url.JoinPath(sasEndpoint, "/folders/folders")
			updateFolderPath := fmt.Sprintf("%s/%s", a, viya[viyaIndex].ID)
			req, _ := http.NewRequest("PUT", updateFolderPath, bytes.NewReader(b))

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("Accept", "application/json, application/vnd.sas.collection+json")
			// Log before auth added
			req.Header.Add("Authorization", "Bearer "+token.AccessToken)

			res, _ := cmd.Client.Do(req)
			if res.StatusCode != 200 {
				return cmd.ExitWithHTTPError(res, "201", "https://developer.sas.com/rest-apis/folders/updateFolder#responses")
			}
		}

		if !equalFolderPrincipalTypesDeep(f.Controls, viya[viyaIndex].Controls) {
			for principalType, principals := range f.Controls {
				viyaPrincipals := viya[viyaIndex].Controls[principalType] // if not found, then id_index will create

				if !equalFolderPrincipals(principals, viyaPrincipals) {
					for principal, b := range principals.Principal { // Prinicipal == everyone|group|authenticatedusers|users
						viyaRule := viyaPrincipals.Principal[principal] // if not found, then id_index will create

						for typ, permissions := range b.Permissions.RuleType {
							viyaPermissions := viyaRule.Permissions.RuleType[typ]

							idIndex := slices.IndexFunc(viyaPermissions, func(str string) bool {
								if len(str) < 3 {
									return false
								}
								return str[:3] == "ID:"
							})
							if idIndex == -1 {
								// create new rule
								patches = append(patches, Patch{
									Op:   "add",
									Path: "/authorization/rules",
									Value: PatchValue{
										ObjectURI:     fmt.Sprintf("/folders/folders/%s", viya[viyaIndex].ID),
										PrincipalType: principalType,
										Principal:     principal,
										Enabled:       true,
										Type:          typ,
										Permissions:   permissions,
									},
								})
								count++
								continue
							}

							ID := viyaPermissions[idIndex][3:]

							if slices.Equal(permissions, slices.DeleteFunc(viyaPermissions, func(str string) bool { return str[:3] == "ID:" })) {
								continue
							}

							// Need to also be able to create rule if principal(?) doesn't exist
							patches = append(patches, Patch{
								Op:   "replace",
								Path: fmt.Sprintf("/authorization/rules/%s", ID),
								Value: PatchValue{
									ObjectURI:     fmt.Sprintf("/folders/folders/%s", viya[viyaIndex].ID),
									PrincipalType: principalType,
									Principal:     principal,
									Enabled:       true,
									Type:          typ,
									Permissions:   permissions,
								},
							})
						}
					}
				}
			}
		}
	}

	for _, f := range newFolders {
		ID, err := createFolder(f.Path, f.Description, sasEndpoint, token, viya)
		if err != nil {
			return fmt.Errorf("unable to create folder: %s", err.Error())
		}

		for principalType, principals := range f.Controls {
			for principal, properties := range principals.Principal {
				for typ, permissions := range properties.Permissions.RuleType {
					patches = append(patches, Patch{
						Op:   "add",
						Path: "/authorization/rules",
						Value: PatchValue{
							ObjectURI:     fmt.Sprintf("/folders/folders/%s", ID),
							PrincipalType: principalType,
							Principal:     principal,
							Enabled:       true,
							Type:          typ,
							Permissions:   permissions,
						},
					})
				}
			}
		}
	}

	if len(patches) > 0 {
		err := applyRulePatches(token, sasEndpoint, "folders", patches)
		return err
	}
	zap.S().Infow("Finished writing folders", "sasEndpoint", sasEndpoint)
	return nil
}

func equalFolderPrincipals(l, r FolderPrincipals) bool {
	if len(l.Principal) != len(r.Principal) {
		return false
	}

	for key, lProp := range l.Principal {
		rProp, found := r.Principal[key]
		if !found {
			return false
		}

		if lProp.Reason != rProp.Reason ||
			lProp.Condition != rProp.Condition ||
			lProp.Description != rProp.Description ||
			lProp.Enabled != rProp.Enabled ||
			lProp.ID != rProp.ID {
			return false
		}

		if !equalPermissions(lProp.Permissions, rProp.Permissions) {
			return false
		}
	}

	return true
}

func equalFolderPrincipalTypesDeep(l, r map[string]FolderPrincipals) bool {
	if len(l) != len(r) {
		return false
	}

	for uri, lPrincipal := range l {
		rPrincipal, found := r[uri]
		if !found {
			return false
		}
		if !equalFolderPrincipals(lPrincipal, rPrincipal) {
			return false
		}
	}

	return true
}
