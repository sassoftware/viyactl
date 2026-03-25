package configtypes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/sassoftware/viyactl/cmd"
	"github.com/sassoftware/viyactl/cmd/environment"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"golang.org/x/crypto/blake2b"
)

// Configs is the set of configs under Environment/Configurations
type Configs map[string]map[string]Definitions

// Definitions are the configuration of each object
// Is only ever either map[string]any or []map[string]any or []any
type Definitions any

// Name returns a human readable name
func (*Configs) Name() string {
	return "configs"
}

// FileName returns the name of the yaml file associated with configs
func (*Configs) FileName() string {
	return "siteconfig.yaml"
}

// Read gets the configs from the given SAS Viya deployment
func (c *Configs) Read(token environment.Token, sasEndpoint string) error {
	var err error

	if cmd.FilterFile != "" {
		var file *os.File
		file, err = os.Open(cmd.FilterFile)
		if err != nil {
			return fmt.Errorf("unable to open filter at %q", cmd.FilterFile)
		}
		defer func() {
			err := file.Close()
			if err != nil {
				zap.S().Warnw("unable to close file", "path", cmd.FilterFile)
			}
		}()
		decoder := yaml.NewDecoder(file)

		for {
			var dc map[string]any
			if err := decoder.Decode(&dc); err != nil {
				break // EOF or error
			}

			var b []byte
			b, _ = yaml.Marshal(dc)

			for k := range dc {
				if k == "configs" {
					cmd.ConfigFilter = b
					break
				}
			}
		}
	}
	if strings.HasPrefix(sasEndpoint, "https://") {
		*c, err = getViyaConfigs(token, sasEndpoint)
	} else {
		*c, err = getLocalConfigs(sasEndpoint)
	}
	return err
}

// Write makes the configs on the SAS Viya environment the same as other, applying the minimum number of changes
func (c *Configs) Write(token environment.Token, sasEndpoint string, other any) error {
	otherConfigs, ok := other.(*Configs)
	if !ok {
		return errors.New("other input to configs.Write must be a Config")
	}
	return configs(token, sasEndpoint, *c, *otherConfigs)
}

// YAML returns a YAML representation of configs
func (c *Configs) YAML() ([]byte, error) {
	// Needed as goccy/go-yaml changes some values from ints to strings in scientific notation, which is incompatible with SAS Viya
	configsBuffer := new(bytes.Buffer)
	enc := &ConfigEncoder{
		writer:             configsBuffer,
		opts:               []yaml.EncodeOption{yaml.IndentSequence(true), yaml.AutoInt(), yaml.Flow(true), yaml.UseSingleQuote(true)},
		customMarshalerMap: map[reflect.Type]func(context.Context, any) ([]byte, error){},
		line:               1,
		column:             1,
		offset:             0,
		indentNum:          DefaultIndentSpaces,
		anchorRefToName:    make(map[uintptr]string),
		anchorNameMap:      make(map[string]struct{}),
		aliasRefToName:     make(map[uintptr]string),
	}

	cons := struct {
		C Configs `yaml:"configs"`
	}{
		C: *c,
	}

	err := enc.Encode(cons)
	// Needed to patch up some multi-line comments until https://github.com/goccy/go-yaml/pull/759 is merged
	configsBuffer = environment.PatchBuffer(configsBuffer)
	b := slices.Concat([]byte("---\n"), configsBuffer.Bytes())
	return b, err
}

// Filter takes a file, reads it and filters the struct inplace
func (c *Configs) Filter(filterPath string) error {
	file, err := os.Open(filterPath)
	if err != nil {
		return fmt.Errorf("unable to open filter at %q", filterPath)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			zap.S().Warnw("unable to close file", "path", filterPath)
		}
	}()
	decoder := yaml.NewDecoder(file)

	var filter Configs
	for {
		var dc map[string]any
		if err := decoder.Decode(&dc); err != nil {
			break // EOF or error
		}

		var b []byte
		b, err = yaml.Marshal(dc)

		for k := range dc {
			if k == c.Name() {
				multierr.AppendInto(&err, yaml.Unmarshal(b, &filter))
				break
			}
		}
	}

	if err != nil {
		return err
	}

	newConfigs := make(Configs)

	for configType, filterEntries := range filter {
		localEntries, ok := filter[configType]
		if !ok {
			continue // Skip if configType doesn't exist in local
		}

		newConfigs[configType] = make(map[string]Definitions)

		for configName := range filterEntries {
			if localDef, ok := localEntries[configName]; ok {
				newConfigs[configType][configName] = localDef
			}
		}

		// If no entries were added for this type, remove the empty map
		if len(newConfigs[configType]) == 0 {
			delete(newConfigs, configType)
		}
	}
	*c = newConfigs
	return nil
}

// Clone creates an empty Caslibs object
func (*Configs) Clone() ConfigType {
	return &Configs{}
}

func init() {
	SupportedTypes = append(SupportedTypes, &Configs{})
}

// Reading

// metadata is the returned metadata associated with a set of configurations
type metadata struct {
	IsDefault bool     `json:"isDefault"` // IsDefault is if a configuration is default, this is true for most internal products
	Services  []string `json:"services"`  // Services are a list of services the contained config applies to, if this list is empty then the configs apply globally
	MediaType string   `json:"mediaType"` // MediaType contains the MediaType and the versioning that is required for setting the setting
}

// item is the schema for the returned data from SAS Viya configuration/configurations
type item struct {
	Version  int            `json:"version"`
	ID       string         `json:"id"`
	Links    []any          `json:"links"`
	Metadata metadata       `json:"metadata"`
	Configs  map[string]any `json:"configs"`
}

var printedPasswordNotification = false

// UnmarshalJSON takes item as JSON and converts it to strongly typed Go struct
func (i *item) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if v, ok := raw["version"]; ok {
		if err := json.Unmarshal(v, &i.Version); err != nil {
			return err
		}
		delete(raw, "version")
	}

	if v, ok := raw["links"]; ok {
		if err := json.Unmarshal(v, &i.Links); err != nil {
			return err
		}
		delete(raw, "links")
	}

	if v, ok := raw["id"]; ok {
		if err := json.Unmarshal(v, &i.ID); err != nil {
			return err
		}
		delete(raw, "id")
	}

	if v, ok := raw["metadata"]; ok {
		if err := json.Unmarshal(v, &i.Metadata); err != nil {
			return err
		}
		delete(raw, "metadata")
	}

	i.Configs = make(map[string]any)

	for k, v := range raw {
		var val any
		decoder := json.NewDecoder(bytes.NewReader(v))
		if err := decoder.Decode(&val); err != nil {
			return err
		}

		switch v := val.(type) {
		case string:
			i.Configs[k] = val
		case float64:
			if v == math.Trunc(v) {
				i.Configs[k] = int64(v)
			}
		}

		if slices.Contains(cmd.RedactedConfigs, k) {
			h := blake2b.Sum512(v)
			i.Configs[k] = fmt.Sprintf("blake2b_512:%x", h)
			if !printedPasswordNotification {
				zap.S().Infow(cmd.Symbols["notify"]+"at least one password field was hashed", "key", k)
				printedPasswordNotification = true
			}

		} else {
			i.Configs[k] = val
		}
	}

	return nil
}

func getViyaConfigs(token environment.Token, sasEndpoint string) (Configs, error) {
	var configurations struct {
		Items []item `json:"items"`
	}

	httpFilter := url.Values{}
	baseURL := fmt.Sprintf("%s/configuration/configurations", sasEndpoint)
	if cmd.ConfigName != "" {
		httpFilter.Set("definitionName", cmd.ConfigName)
	}
	if cmd.ConfigService != "" {
		httpFilter.Set("serviceName", cmd.ConfigService)
	}

	perform := baseURL + "?" + httpFilter.Encode()

	req, _ := http.NewRequest("GET", perform, nil)
	req.Header.Add("Authorization", "BEARER "+token.AccessToken)
	req.Header.Add("Accept", "application/json, application/vnd.sas.api+json")
	req.Header.Add("Content-Type", "application/json")
	resp, _ := cmd.Client.Do(req)

	a, _ := io.ReadAll(resp.Body)

	err := json.Unmarshal(a, &configurations)
	if err != nil {
		zap.S().Infow("Unable to unmarshal returned JSON", "JSON", string(a))
		return Configs{}, fmt.Errorf("TODO(contact viyactl support) unable to unmarshal returned JSON: %s", "1")
	}

	viyaConfig := Configs{}

	for _, item := range configurations.Items {
		// MediaType will be something like: application/vnd.sas.configuration.config.sas.batch.server+json;version=1, but only need sas.batch.server
		definition, _ := strings.CutPrefix(item.Metadata.MediaType, "application/vnd.sas.configuration.config.")
		definition, _, _ = strings.Cut(definition, "+")

		if len(item.Metadata.Services) == 0 {
			if _, ok := viyaConfig["global"]; !ok {
				viyaConfig["global"] = map[string]Definitions{}
			}

			if _, found := item.Configs["name"]; found {
				// Check if it's already a slice
				if existing, ok := viyaConfig["global"][definition].([]map[string]any); ok {
					viyaConfig["global"][definition] = append(existing, item.Configs)
				} else {
					viyaConfig["global"][definition] = []map[string]any{item.Configs}
				}
			} else {
				viyaConfig["global"][definition] = item.Configs
			}
		}

		for _, service := range item.Metadata.Services {
			if _, ok := viyaConfig[service]; !ok {
				viyaConfig[service] = map[string]Definitions{}
			}

			if _, found := item.Configs["name"]; found {
				if existing, ok := viyaConfig[service][definition].([]map[string]any); ok {
					viyaConfig[service][definition] = append(existing, item.Configs)
				} else {
					viyaConfig[service][definition] = []map[string]any{item.Configs}
				}
			} else {
				viyaConfig[service][definition] = item.Configs
			}
		}
	}

	if cmd.FilterFile != "" {
		err = viyaConfig.Filter(cmd.FilterFile)
	}

	zap.S().Infow("Finished reading configs", "endpoint", sasEndpoint)
	return viyaConfig, err
}

func getLocalConfigs(path string) (Configs, error) {
	c := Configs{}
	buf, err := os.ReadFile(filepath.Join(path, c.FileName()))
	if err != nil {
		return c, fmt.Errorf("unable to read from %q", path)
	}

	con := struct {
		Conf Configs `json:"configs"`
	}{}

	err = yaml.Unmarshal(buf, &con)
	if err != nil {
		return c, fmt.Errorf("unable to unmarshal from %q", path)
	}

	if cmd.FilterFile != "" {
		err = con.Conf.Filter(cmd.FilterFile)
	}

	return con.Conf, err
}

// Writing

// configurationByType is a mapping of the metadata to a list of configurations
type configurationByType struct {
	Metadata metadata       `json:"metadata"` // Metadata is the associated metadata for the set of configurations
	Configs  map[string]any `json:",inline"`  // Configs is a map of configurations to their values, may be nested
}

// MarshalJSON defines how to put a struct into json
func (c configurationByType) MarshalJSON() ([]byte, error) {
	result := make(map[string]any)
	result["metadata"] = c.Metadata

	for k, v := range c.Configs {
		if b, ok := v.([]byte); ok {
			var raw any
			if err := json.Unmarshal(b, &raw); err == nil {
				result[k] = raw
				continue
			}
		}
		result[k] = v
	}

	return json.Marshal(result)
}

type definition struct {
	Version int    `json:"definitionVersion"`
	Name    string `json:"name"`
}

func getDefinitionVersions(token environment.Token, sasEndpoint string) (map[string]int, error) {
	zap.S().Infow("Getting definition versions", "sasEndpoint", sasEndpoint)
	var definitions struct {
		Items []definition `json:"items"`
	}
	defsURL := fmt.Sprintf("%s/configuration/definitions", sasEndpoint)

	req, _ := http.NewRequest("GET", defsURL, nil)
	req.Header.Add("Authorization", "BEARER "+token.AccessToken)
	req.Header.Add("Accept", "application/json, application/vnd.sas.collection+json;version=2")
	req.Header.Add("Content-Type", "application/json")

	resp, _ := cmd.Client.Do(req)

	a, _ := io.ReadAll(resp.Body)
	err := json.Unmarshal(a, &definitions)
	if err != nil {
		zap.S().Infow("Unable to unmarshal returned JSON", "JSON", string(a))
		return nil, fmt.Errorf("TODO(contact viyactl support) unable to unmarshal returned JSON: %s", "1")
	}

	defsMap := make(map[string]int, len(definitions.Items))
	for _, it := range definitions.Items {
		if v, found := defsMap[it.Name]; found {
			if it.Version > v {
				defsMap[it.Name] = it.Version
			}
		} else {
			defsMap[it.Name] = it.Version
		}
	}

	zap.S().Infow("Got definition versions", "sasEndpoint", sasEndpoint)
	return defsMap, nil
}

func configs(token environment.Token, sasEndpoint string, viya, local Configs) error {
	patch := []configurationByType{}
	create := []configurationByType{}

	defsMap, err := getDefinitionVersions(token, sasEndpoint)
	if err != nil {
		return fmt.Errorf("unable to retrieve definition versions: %s", err.Error())
	}

	for service, definitions := range local {
		for definition, properties := range definitions {
			newVersion := defsMap[definition]

			var propsList []map[string]any
			switch p := properties.(type) {
			case []map[string]any:
				propsList = p
			case []any:
				for _, item := range p {
					if m, ok := item.(map[string]any); ok {
						propsList = append(propsList, m)
					}
				}
			case map[string]any:
				propsList = []map[string]any{p}
			default:
				zap.S().Warnf("cannot write service due to incorrect format (should be []map[string]any, []any or map[string]any)", "service", service, "property", properties)
				continue
			}

			for _, props := range propsList {
				var existing map[string]any
				if v, found := viya[service][definition]; found {
					switch vv := v.(type) {
					case []map[string]any:
						for _, candidate := range vv {
							if candidate["name"] == props["name"] {
								existing = candidate
								break
							}
						}
					case map[string]any:
						existing = vv
					}
				}

				if existing != nil {
					maps.DeleteFunc(existing, func(key string, _ any) bool {
						return key == "version"
					})

					for _, key := range cmd.RedactedConfigs {
						if p, ok := existing[key]; ok {
							if pString, ok := p.(string); ok {
								h := blake2b.Sum512([]byte(pString))
								if fmt.Sprintf("blake2b_512:%x", h) == pString {
									continue
								}
							}
						}
					}

					if !reflect.DeepEqual(existing, props) {
						if _, found := existing["id"]; !found {
							zap.S().Infow(cmd.Symbols["notify"]+"unable to get id", "service", service, "definition", definition)
							continue
						}

						propsWithID := map[string]any{}
						maps.Copy(propsWithID, props)
						propsWithID["id"] = existing["id"]

						metadata := metadata{
							IsDefault: false,
							MediaType: fmt.Sprintf("application/vnd.sas.configuration.config.%s+json;version=%d", definition, newVersion),
						}
						if service != "global" {
							metadata.Services = []string{service}
						}

						patch = append(patch, configurationByType{
							Metadata: metadata,
							Configs:  propsWithID,
						})
					}
				} else {
					metadata := metadata{
						IsDefault: false,
						MediaType: fmt.Sprintf("application/vnd.sas.configuration.config.%s+json;version=%d", definition, newVersion),
					}
					if service != "global" {
						metadata.Services = []string{service}
					}

					create = append(create, configurationByType{
						Metadata: metadata,
						Configs:  props,
					})
				}
			}
		}
	}

	configURL, _ := url.JoinPath(sasEndpoint, "/configuration/configurations")

	if len(create) > 0 {
		zap.S().Infow("Creating configs", "sasEndpoint", sasEndpoint)
		creates := struct {
			Version int                   `json:"version"`
			Name    string                `json:"name"`
			Items   []configurationByType `json:"items"`
		}{
			Version: 2,
			Name:    "viyactl-appplication-configs",
			Items:   create,
		}

		b, err := json.Marshal(creates)
		if err != nil {
			return fmt.Errorf("TODO(contact viyactl support) unable to marshal patch to json: %s", err.Error())
		}

		req, err := http.NewRequest("POST", configURL, bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("unable to write configs: %s", err)
		}

		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Accept", "application/json, application/vnd.sas.collection+json")
		// Log before auth added
		req.Header.Add("Authorization", "Bearer "+token.AccessToken)

		res, err := cmd.Client.Do(req)
		if err != nil {
			return fmt.Errorf("unable to write configs: %s", err)
		}
		if res.StatusCode == 400 {
			return cmd.ExitWithHTTPError(res, "20{0|1|4}", "https://developer.sas.com/rest-apis/configuration/createConfigurations#responses")
		}

		zap.S().Infow("Finished creating configs", "sasEndpoint", sasEndpoint)
	}

	if len(patch) > 0 {
		zap.S().Infow("Patching configs", "sasEndpoint", sasEndpoint)
		patches := struct {
			Version int                   `json:"version"`
			Name    string                `json:"name"`
			Items   []configurationByType `json:"items"`
		}{
			Version: 2,
			Name:    "viyactl-appplication-configs",
			Items:   patch,
		}
		zap.S().Infow("patch created", "number of items to be patched", len(patches.Items))

		b, err := json.Marshal(patches)
		if err != nil {
			return fmt.Errorf("TODO(contact viyactl support) unable to marshal patch to json: %s", err.Error())
		}

		req, err := http.NewRequest("PATCH", configURL, bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("unable to write configs: %s", err.Error())
		}

		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Accept", "application/json, application/vnd.sas.collection+json")
		req.Header.Add("Authorization", "Bearer "+token.AccessToken)

		res, err := cmd.Client.Do(req)
		if err != nil {
			return fmt.Errorf("unable to write configs: %s", err.Error())
		}
		zap.S().Infow("Finished writing configs", "url", sasEndpoint)
		if res.StatusCode == 400 {
			return cmd.ExitWithHTTPError(res, "20{0|1|4}", "https://developer.sas.com/rest-apis/configuration/patchConfigurations#responses")
		}
		zap.S().Infow("Finished patching configs", "sasEndpoint", sasEndpoint)
	}
	zap.S().Infow("Finished writing configs", "sasEndpoint", sasEndpoint)
	return nil
}
