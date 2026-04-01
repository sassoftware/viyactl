// Copyright © 2026, SAS Institute Inc., Cary, NC, USA.  All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
// Package configtypes provides a mechanism for managing arbitrarily many types of comfiguration
package configtypes

import (
	"slices"

	"github.com/sassoftware/viyactl/cmd"
	"github.com/sassoftware/viyactl/cmd/environment"
)

// SupportedTypes is the slice of registered configtypes, to extend viyactl with new config types, add them to this slice
var SupportedTypes []ConfigType

// ConfigType is the interface that all configtypes must implement
type ConfigType interface {
	// Name returns the human readable name of the configtype (e.g. 'groups' or 'rules') and is used in help text and errors
	Name() string
	// FileName returns the name of the yaml file that is associated with the configtype (e.g. 'siterules.yaml') and is used when reading frm/writing to a directory
	FileName() string
	// Read should get the appropriate configs from a SAS Viya deployment, and mutate the type inplace
	Read(token environment.Token, sasEndpoint string) error
	// Write should apply the posterior configs to a SAS Viya deployment
	Write(token environment.Token, sasEndpoint string, posterior any) error
	// YAML returns the YAML representation of the struct, MarshalYAML is not used here as some ConfigTypes may require additional steps to Marshal
	YAML() ([]byte, error)
	// Filter takes a file, reads it and filters the struct inplace
	Filter(filterPath string) error
	// Clone creates an empty instance of the struct
	Clone() ConfigType
}

// NewEnv creates a clone of SupportedTypes, and is used when multiple environments are specified (e.g. Diff, Report, Write)
func NewEnv() []ConfigType {
	dst := make([]ConfigType, len(SupportedTypes))
	for i, ct := range SupportedTypes {
		dst[i] = ct.Clone()
	}
	return dst
}

func init() {
	for _, configType := range cmd.Disable {
		SupportedTypes = slices.DeleteFunc(SupportedTypes, func(s ConfigType) bool { return s.Name() == configType })
	}
}
