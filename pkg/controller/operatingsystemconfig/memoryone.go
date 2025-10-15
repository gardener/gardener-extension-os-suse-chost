// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig

import (
	"fmt"
	"strings"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"

	memoryonechost "github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost/v1alpha1"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/memoryone"
)

const (
	memoryTopology = "mem_topology"
	systemMemory   = "system_memory"
)

func wrapIntoMemoryOneHeaderAndFooter(osc *extensionsv1alpha1.OperatingSystemConfig, in string) (string, error) {
	config, err := memoryone.Configuration(osc)
	if err != nil {
		return "", err
	}

	memoryOneConfiguration := vsmpConfigString(config)

	out := `Content-Type: multipart/mixed; boundary="==BOUNDARY=="
MIME-Version: 1.0
--==BOUNDARY==
Content-Type: text/x-vsmp; section=vsmp

` + memoryOneConfiguration + `
--==BOUNDARY==
Content-Type: text/x-shellscript

` + in + `
--==BOUNDARY==--
`

	return out, nil
}

func vsmpConfigString(config *memoryonechost.OperatingSystemConfiguration) string {
	var vsmpConfiguration map[string]string
	var configStringBuilder strings.Builder

	if config != nil && config.VsmpConfiguration != nil {
		vsmpConfiguration = config.VsmpConfiguration
		// TODO: put stripSemicola down into the StringBuilder-Fprintf and remove this loop once we end support for legacy values
		// this is required as we do not want to allow injecting key-value pairs with semicola in the new parameter map
		// but need to retain the previous behaviour for the legacy configuration style
		for k, v := range vsmpConfiguration {
			vsmpConfiguration[k] = stripSemicola(v)
		}
	} else {
		vsmpConfiguration = make(map[string]string, 2)
	}

	if _, ok := vsmpConfiguration[memoryTopology]; !ok {
		vsmpConfiguration[memoryTopology] = "2"
	}

	if _, ok := vsmpConfiguration[systemMemory]; !ok {
		vsmpConfiguration[systemMemory] = "6x"
	}

	// TODO: remove these once the transition to VsmpConfiguration map[string]string is complete
	if config != nil {
		if config.SystemMemory != nil {
			vsmpConfiguration[systemMemory] = *config.SystemMemory
		}

		if config.MemoryTopology != nil {
			vsmpConfiguration[memoryTopology] = *config.MemoryTopology
		}
	}
	// end TODO

	for k, v := range vsmpConfiguration {
		fmt.Fprintf(&configStringBuilder, "%s=%s\n", stripSemicola(k), v)
	}

	return configStringBuilder.String()
}

func stripSemicola(s string) string {
	before, _, found := strings.Cut(s, ";")
	if found {
		return before
	}
	return s
}
