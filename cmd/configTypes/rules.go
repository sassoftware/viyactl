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

// Rules represents the Rules of a Viya 4 deployment
type Rules struct {
	ObjectURI map[string]PrincipalTypes `json:"rules"` // Object URI to Rules applying to it
}

// PrincipalTypes represents the Principal types associated with an Object URI under the Rules object
type PrincipalTypes struct {
	PrincipalType map[string]Principals `yaml:",inline" validate:"dive,keys,supportedPrincipalTypes,endkeys"` // Principal type to rules
}

// Principals represents the principals associated with a principal type, the principal is non-empty if the rule only pertains to that specific user
//
// This gets marshaled to the following
//
//	principal:
//	  - id: unique-identifier
//	    enabled: true
//	    permissions:
//	    grant:
//	      - add
type Principals struct {
	Principal map[string][]Properties `yaml:",inline" validate:"required"` // Principal to the Rule types
}

// Properties defines properties that relate to a rule
//
// This is marshaled to the following
//
//	enabled: true
//	reason: String explaining why the rule exists
//	condition: func() bool {}
//	description: String explaining what the rule does
//	permissions:
//	  prohibit:
//	    - read
//	    - create
type Properties struct {
	ID          string      `json:"-"`
	Enabled     bool        `json:"enabled,omitempty"`
	Reason      string      `json:",omitempty"`
	Condition   string      `json:"condition,omitempty"`
	Description string      `json:",omitempty"`
	Permissions Permissions `validate:"required"`
}

// MarshalYAML defines how a struct gets written to a YAML string
//
// Properties omits the ID to not clutter the YAML file
func (p Properties) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(&struct {
		Reason      string      `json:",omitempty"`
		Condition   string      `json:"condition,omitempty"`
		Description string      `json:",omitempty"`
		Permissions Permissions `validate:"required"`
	}{
		p.Reason,
		p.Condition,
		p.Description,
		p.Permissions,
	})
}

// Permissions represents the list of rules that are granted
//
// This represents the following part of siterules/sitefolders
//
//	permissions:
//	  grant:
//	    - add
//	    - delete
//	    - read
//	    - remove
//	  prohibit:
//	    - secure
//	    - create
//	    - update
//
// Supported rule types are described in [SupportedRuleTypes]
//
// Supported permissions are described in [SupportedPermissions]
type Permissions struct {
	RuleType map[string][]string `json:",omitempty" yaml:",inline" validate:"dive,keys,supportedRuleTypes,endkeys"`
}

// MarshalYAML defines how a struct gets written to a YAML string
//
// Properties omits the ID to not clutter the YAML file
func (p Permissions) MarshalYAML() ([]byte, error) {
	if len(p.RuleType) == 0 {
		return []byte{}, nil
	}

	out := make(map[string][]string, len(p.RuleType))
	for k, v := range p.RuleType {
		newV := slices.DeleteFunc(v, func(str string) bool {
			if len(str) < 3 {
				return false
			}
			return strings.HasPrefix(k, "ID:")
		})
		out[k] = newV
	}

	if len(out) == 0 {
		return []byte("null\n"), nil
	}

	return yaml.Marshal(out)
}

// Name returns a human readable name
func (*Rules) Name() string {
	return "rules"
}

// FileName returns the name of the yaml file associated with rules
func (*Rules) FileName() string {
	return "siterules.yaml"
}

// Read gets the rules from the given SAS Viya deployment
func (r *Rules) Read(token environment.Token, sasEndpoint string) error {
	var err error

	if cmd.FilterFile != "" {
		filter, err := getRuleFilterFromFile(cmd.FilterFile)
		if err != nil {
			return err
		}
		for objectURI, principalTypes := range filter.ObjectURI {
			for principalType, principalMap := range principalTypes.PrincipalType {
				for principal := range principalMap.Principal {
					cmd.RuleFilters = append(cmd.RuleFilters, fmt.Sprintf("and(eq(objectUri,'%s'),eq(principalType,'%s'),eq(principal,'%s'))", objectURI, principalType, principal))
				}
				if len(principalMap.Principal) == 0 {
					cmd.RuleFilters = append(cmd.RuleFilters, fmt.Sprintf("and(eq(objectUri,'%s'),eq(principalType,'%s'))", objectURI, principalType))
				}
			}

			if len(principalTypes.PrincipalType) == 0 {
				cmd.RuleFilters = append(cmd.RuleFilters, fmt.Sprintf("eq(objectUri,'%s')", objectURI))
			}
		}
	}

	if strings.HasPrefix(sasEndpoint, "https://") {
		*r, err = getViyaRules(token, sasEndpoint)
	} else {
		*r, err = getLocalRules(sasEndpoint)
	}
	return err
}

// Write makes the rules on the SAS Viya environment the same as other, applying the minimum number of changes
func (r *Rules) Write(token environment.Token, sasEndpoint string, other any) error {
	otherRules, ok := other.(*Rules)
	if !ok {
		return errors.New("other input to rules.Write must be a Group")
	}
	return rules(token, sasEndpoint, *r, *otherRules)
}

// YAML returns a YAML representation of rules
func (r *Rules) YAML() ([]byte, error) {
	b, err := yaml.MarshalWithOptions(r, yaml.IndentSequence(true), yaml.AutoInt())
	b = slices.Concat([]byte("---\n"), b)
	return b, err
}

func getRuleFilterFromFile(filterPath string) (Rules, error) {
	file, err := os.Open(filterPath)
	if err != nil {
		return Rules{}, fmt.Errorf("unable to open filter at %q", filterPath)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			zap.S().Warnw("unable to close file", "path", filterPath)
		}
	}()
	decoder := yaml.NewDecoder(file)

	var filter Rules
	for {
		var dc map[string]any
		if err := decoder.Decode(&dc); err != nil {
			break // EOF or error
		}

		var b []byte
		b, err = yaml.Marshal(dc)

		for k := range dc {
			if k == "rules" {
				multierr.AppendInto(&err, yaml.Unmarshal(b, &filter))
				break
			}
		}
	}

	return filter, err
}

// Filter takes a file, reads it and filters the struct inplace
func (r *Rules) Filter(filterPath string) error {
	filter, err := getRuleFilterFromFile(filterPath)
	*r = filterRules(*r, filter)
	return err
}

// Clone creates an empty Rules object
func (*Rules) Clone() ConfigType {
	return &Rules{}
}

func init() {
	SupportedTypes = append(SupportedTypes, &Rules{})
}

// Reading

// viyaRule is the rule associated with an Object/Container
//
// Derived from Resource Collection of Authorization Rules > items at
// https://developer.sas.com/rest-apis/authorization/schemas#resource-collection-of-authorization-rules
type viyaRule struct {
	ObjectURI     string   `json:"objectURI"`
	ContainerURI  string   `json:"containerURI"`
	Permissions   []string `json:"permissions"`
	PrincipalType string   `json:"principalType"`
	Type          string   `json:"type"`
	Principal     string   `json:"principal"`
	ID            string   `json:"id"`
	Enabled       bool     `json:"ruleStatus"`
	Condition     string   `json:"condition,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Description   string   `json:"description,omitempty"`
}

// UnmarshalJSON for a viyaRule changes the default value of enabled to be true
// all rules are enabled, unless explicitly disabled
func (t *viyaRule) UnmarshalJSON(data []byte) error {
	type innerRule viyaRule
	inner := &innerRule{
		Enabled: true,
	}

	if err := json.Unmarshal(data, inner); err != nil {
		return err
	}

	*t = viyaRule(*inner)
	return nil
}

// collectPaginatedRules collects all rules from a SAS Viya deployment
func collectPaginatedRules(token environment.Token, sasEndpoint string, filter url.Values) ([]viyaRule, error) {
	var items struct {
		Count     int        `json:"count"`
		ViyaRules []viyaRule `json:"items"`
	}
	baseURL := fmt.Sprintf("%s/authorization/rules", sasEndpoint)

	filter.Set("start", strconv.Itoa(0))
	perform := baseURL + "?" + filter.Encode()

	req, _ := http.NewRequest("GET", perform, nil)
	req.Header.Add("Authorization", "BEARER "+token.AccessToken)
	req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
	req.Header.Add("Content-Type", "application/json")

	zap.S().Info("Getting first page of rules")
	resp, _ := cmd.Client.Do(req)
	a, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return nil, cmd.ExitWithHTTPError(resp, "200", "https://developer.sas.com/rest-apis/identities/getGroups")
	}

	err := json.Unmarshal(a, &items)
	if err != nil {
		zap.S().Infow("Unable to unmarshal returned JSON", "JSON", string(a))
		return nil, fmt.Errorf("TODO(contact viyactl support) unable to unmarshal returned JSON: %q", string(a))
	}
	zap.S().Infow("Got total number of rules under filter", "total rules", len(items.ViyaRules), "filter", filter)
	if items.Count-len(items.ViyaRules) > 0 {
		items.ViyaRules = slices.Grow(items.ViyaRules, items.Count-len(items.ViyaRules))
	}

	wg, _ := errgroup.WithContext(context.Background())

	for current := len(items.ViyaRules); current < items.Count; current += 50 {
		wg.Go(func() error {
			filter2 := url.Values{}
			maps.Copy(filter2, filter)
			filter2.Set("start", strconv.Itoa(current))
			perform := baseURL + "?" + filter2.Encode()

			req, err := http.NewRequest("GET", perform, nil)
			if err != nil {
				return fmt.Errorf("unable to create http request, %q", err.Error())
			}
			req.Header.Add("Authorization", "BEARER "+token.AccessToken)
			req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
			req.Header.Add("Content-Type", "application/json")

			resp, err := cmd.Client.Do(req)
			if err != nil {
				return fmt.Errorf("unable to perform request, %q", err.Error())
			}

			a, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("unable to read response body, %q", err.Error())
			}
			var items2 struct {
				Count     int        `json:"count"`
				ViyaRules []viyaRule `json:"items"`
			}

			err = json.Unmarshal(a, &items2)
			if err != nil {
				zap.S().Infow("Unable to unmarshal returned JSON", "JSON", string(a))
				return fmt.Errorf("TODO(contact viyactl support) unable to unmarshal returned JSON: %q", string(a))
			}

			items.ViyaRules = append(items.ViyaRules, items2.ViyaRules...)
			return nil
		})
	}

	err = wg.Wait()
	zap.S().Infow("Got all rules under filter", "filter", filter)
	return items.ViyaRules, err
}

func getViyaRules(token environment.Token, sasEndpoint string) (Rules, error) {
	viya := Rules{}

	filter := url.Values{}

	f := "not(startsWith(objectUri,'/folders/'))"
	if len(cmd.RuleFilters) > 0 {
		f = fmt.Sprintf("or(%s)", strings.Join(cmd.RuleFilters, ","))
		f = fmt.Sprintf("and(%s,%s)", f, "not(startsWith(objectUri,'/folders/'))")
	}
	filter.Set("filter", f)

	items, err := collectPaginatedRules(token, sasEndpoint, filter)
	if err != nil {
		return viya, err
	}
	zap.S().Info("Parsing rules to viyactl representation")

	viya.ObjectURI = make(map[string]PrincipalTypes)
	for _, item := range items {
		for i, p := range item.Permissions {
			item.Permissions[i] = strings.ToLower(p)
		}

		if _, found := viya.ObjectURI[item.ObjectURI]; !found {
			viya.ObjectURI[item.ObjectURI] = PrincipalTypes{
				map[string]Principals{
					item.PrincipalType: {
						Principal: map[string][]Properties{
							item.Principal: {
								{
									ID:          item.ID,
									Enabled:     item.Enabled,
									Reason:      item.Reason,
									Condition:   item.Condition,
									Description: item.Description,
									Permissions: Permissions{RuleType: map[string][]string{item.Type: item.Permissions}},
								},
							},
						},
					},
				},
			}
			continue
		}

		// Object URI already exists
		if _, found := viya.ObjectURI[item.ObjectURI].PrincipalType[item.PrincipalType]; !found {
			viya.ObjectURI[item.ObjectURI].PrincipalType[item.PrincipalType] = Principals{
				Principal: map[string][]Properties{
					item.Principal: {
						{
							ID:          item.ID,
							Enabled:     item.Enabled,
							Reason:      item.Reason,
							Condition:   item.Condition,
							Description: item.Description,
							Permissions: Permissions{RuleType: map[string][]string{item.Type: item.Permissions}},
						},
					},
				},
			}
			continue
		}

		// Object URI & Principal already exist
		existingProps := viya.ObjectURI[item.ObjectURI].PrincipalType[item.PrincipalType].Principal[item.Principal]
		found := false

		for i, prop := range existingProps {
			if prop.ID == item.ID {
				if _, ok := prop.Permissions.RuleType[item.Type]; !ok {
					existingProps[i].Permissions.RuleType[item.Type] = item.Permissions
				}
				found = true
				break
			}
		}

		if !found {
			viya.ObjectURI[item.ObjectURI].PrincipalType[item.PrincipalType].Principal[item.Principal] = append(existingProps, Properties{
				ID:          item.ID,
				Enabled:     item.Enabled,
				Reason:      item.Reason,
				Condition:   item.Condition,
				Description: item.Description,
				Permissions: Permissions{RuleType: map[string][]string{item.Type: item.Permissions}},
			})
		}
	}

	if cmd.FilterFile != "" {
		err = viya.Filter(cmd.FilterFile)
	}

	return viya, err
}

func getLocalRules(path string) (Rules, error) {
	var local Rules
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

func filterRules(local, filter Rules) Rules {
	if len(filter.ObjectURI) == 0 {
		return local
	}
	newRules := Rules{
		ObjectURI: make(map[string]PrincipalTypes),
	}

	for objectURI, principalTypes := range filter.ObjectURI {
		// If filter.ObjectURI[objectURI] is empty, keep all from local
		if len(principalTypes.PrincipalType) == 0 {
			if localPrincipalTypes, ok := local.ObjectURI[objectURI]; ok {
				newRules.ObjectURI[objectURI] = localPrincipalTypes
			}
			continue
		}

		for principalType, principalMap := range principalTypes.PrincipalType {
			// If filter.ObjectURI[objectURI].PrincipalType[principalType] is empty, keep all from local
			if len(principalMap.Principal) == 0 {
				if localPrincipalTypes, ok := local.ObjectURI[objectURI]; ok {
					if localPrincipalMap, ok := localPrincipalTypes.PrincipalType[principalType]; ok {
						if _, exists := newRules.ObjectURI[objectURI]; !exists {
							newRules.ObjectURI[objectURI] = PrincipalTypes{
								PrincipalType: make(map[string]Principals),
							}
						}
						newRules.ObjectURI[objectURI].PrincipalType[principalType] = localPrincipalMap
					}
				}
				continue
			}

			for principal := range principalMap.Principal {
				if localPrincipalTypes, ok := local.ObjectURI[objectURI]; ok {
					if localPrincipalMap, ok := localPrincipalTypes.PrincipalType[principalType]; ok {
						if localProperties, ok := localPrincipalMap.Principal[principal]; ok {
							if _, exists := newRules.ObjectURI[objectURI]; !exists {
								newRules.ObjectURI[objectURI] = PrincipalTypes{
									PrincipalType: make(map[string]Principals),
								}
							}
							if _, exists := newRules.ObjectURI[objectURI].PrincipalType[principalType]; !exists {
								newRules.ObjectURI[objectURI].PrincipalType[principalType] = Principals{
									Principal: make(map[string][]Properties),
								}
							}
							newRules.ObjectURI[objectURI].PrincipalType[principalType].Principal[principal] = localProperties
						}
					}
				}
			}
		}
	}

	return newRules
}

// Writing

// Patch is a JSON patch to rules as specified in https://developer.sas.com/rest-apis/authorization/patchRules
type Patch struct {
	Op    string     `json:"op"`
	Path  string     `json:"path"`
	Value PatchValue `json:"value"`
}

// PatchValue is the 'value struct' within a json patch
type PatchValue struct {
	Permissions   []string `json:"permissions"`
	Type          string   `json:"type"`
	ObjectURI     string   `json:"objectUri"`
	PrincipalType string   `json:"principalType"`
	Principal     string   `json:"principal"`
	Enabled       bool     `json:"enabled"`
}

func equalPrincipals(l, r Principals) bool {
	for lPrincipalType, lPropertiesList := range l.Principal {
		rPropertiesList, found := r.Principal[lPrincipalType]
		if !found || len(lPropertiesList) != len(rPropertiesList) {
			return false
		}

		for _, lProperties := range lPropertiesList {
			matchFound := false
			for _, rProperties := range rPropertiesList {
				if lProperties.Reason == rProperties.Reason &&
					lProperties.Condition == rProperties.Condition &&
					lProperties.Description == rProperties.Description {

					// Compare permissions
					if !equalPermissions(lProperties.Permissions, rProperties.Permissions) {
						continue
					}
					matchFound = true
					break
				}
			}
			if !matchFound {
				return false
			}
		}
	}
	return true
}

func equalPermissions(l, r Permissions) bool {
	if len(l.RuleType) != len(r.RuleType) {
		return false
	}
	for lRuleType, lRules := range l.RuleType {
		rRules, found := r.RuleType[lRuleType]
		if !found || len(lRules) != len(rRules) {
			return false
		}
		for _, v := range lRules {
			if !slices.Contains(rRules, v) {
				return false
			}
		}
	}
	return true
}

func equalPrincipalTypesDeep(l, r PrincipalTypes) bool {
	for lObjectURI, lPrincipal := range l.PrincipalType {
		rPrincipal, found := r.PrincipalType[lObjectURI]
		if !found {
			return false
		}
		if found := equalPrincipals(lPrincipal, rPrincipal); !found {
			return false
		}
	}
	return true
}

func rules(token environment.Token, sasEndpoint string, viya, local Rules) error {
	patches := []Patch{}

	// If in viya but not local, disable
	// for objectURI, v := range viya.ObjectURI {
	// 	if _, found := local.ObjectURI[objectURI]; !found {
	// 		for _, v1 := range v.PrincipalType {
	// 			for _, details := range v1.Principal {
	// 				for _, detail := range details {
	// 					patches = append(patches, Patch{
	// 						Op:    "replace",
	// 						Path:  "/authorization/rules/" + detail.ID,
	// 						Value: PatchValue{Enabled: false},
	// 					})
	// 				}
	// 			}
	// 		}
	// 	}
	// }

	for objectURI, v := range local.ObjectURI {
		if viyaPrincipalType, found := viya.ObjectURI[objectURI]; found {
			if equalPrincipalTypesDeep(v, viyaPrincipalType) {
				continue // Already exists as is
			}
		}

		for principalType, v1 := range v.PrincipalType {
			for principal, details := range v1.Principal {
				if principal == "" && principalType != "everyone" && principalType != "authenticatedUsers" {
					zap.S().Warn(cmd.Symbols["notify"]+`Cannot alter rules without a principal where principalType is not "everyone" or "authenticatedUsers"`, "principalType", principalType)
					continue
				}

				viyaP := viya.ObjectURI[objectURI].PrincipalType[principalType].Principal[principal]

				for _, detail := range details {
					for typ, permissions := range detail.Permissions.RuleType {

						op := "add"
						ID := "" // null ID for add

						// Match ids on grant or prohibit
						for _, detail := range viyaP {
							for viyaType := range detail.Permissions.RuleType {
								if viyaType == typ {
									op = "replace"
									ID = detail.ID
								}
							}
						}

						patches = append(patches, Patch{
							Op:   op,
							Path: "/authorization/rules/" + ID,
							Value: PatchValue{
								ObjectURI:     objectURI,
								PrincipalType: principalType,
								Principal:     principal,
								Enabled:       detail.Enabled,
								Type:          typ,
								Permissions:   permissions,
							},
						})
					}
				}
			}
		}
	}
	return applyRulePatches(token, sasEndpoint, "rules", patches)
}

func applyRulePatches(token environment.Token, sasEndpoint, caller string, patches []Patch) error {
	if len(patches) == 0 {
		return nil
	}
	rulesURL, _ := url.JoinPath(sasEndpoint, "/authorization/rules")

	wg, _ := errgroup.WithContext(context.Background())
	for p := range slices.Chunk(patches, 50) {
		wg.Go(func() error {
			b, err := json.Marshal(p)
			if err != nil {
				return fmt.Errorf("TODO(contact viyactl support) unable to marshal patch to json")
			}

			req, _ := http.NewRequest("PATCH", rulesURL, bytes.NewReader(b))

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("Accept", "application/json, application/vnd.sas.collection+json")
			req.Header.Add("Authorization", "Bearer "+token.AccessToken)

			res, _ := cmd.Client.Do(req)
			if res.StatusCode != 200 {
				zap.S().Error("Encountered error while patching rules", "caller", caller)
				return cmd.ExitWithHTTPError(res, "201", "https://developer.sas.com/rest-apis/authorization/patchRules#responses")
			}
			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return fmt.Errorf("failed to apply patch: %s", err.Error())
	}

	zap.S().Infow("Finished writing rules", "sasEndpoint", sasEndpoint)
	return nil
}
