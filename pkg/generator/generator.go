// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package generator

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost"
	memoryonechostinstall "github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost/install"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/susechost"

	controllercmd "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	oscommontemplate "github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/template"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

const (
	// OSConfigFormat format of the OSC to be generated. Must match the name of a subdirectory under
	// the 'templates' directory. Presently 'script' and 'cloud-init' are supported
	OSConfigFormat = "OS_CONFIG_FORMAT"
	// OSConfigFormatScript is a constant for the 'script' config format.
	OSConfigFormatScript = "script"
	// OSConfigFormatCloudInit is a constant for the 'cloud-init' config format.
	OSConfigFormatCloudInit = "cloud-init"

	// BootCommand command to be executed to bootstap the OS Configuration.
	// Depends on the OSC format and the infrastructure platform.
	// Well known valid values are `"/bin/bash %s"` and `"/usr/bin/cloud-init clean && /usr/bin/cloud-init --file %s init"`.
	BootCommand = "BOOT_COMMAND"
	// BootCommandBash is a constant for the /bin/bash boot command.
	BootCommandBash = "/bin/bash %s"
)

//go:embed templates/*
var templates embed.FS

var decoder runtime.Decoder

func init() {
	scheme := runtime.NewScheme()
	if err := memoryonechostinstall.AddToScheme(scheme); err != nil {
		controllercmd.LogErrAndExit(err, "Could not update scheme")
	}
	decoder = serializer.NewCodecFactory(scheme).UniversalDecoder()
}

// NewCloudInitGenerator creates a new Generator using the template file for suse-chost
func NewCloudInitGenerator() (*oscommontemplate.CloudInitGenerator, error) {
	configFormat, ok := os.LookupEnv(OSConfigFormat)
	if !ok || configFormat == "" {
		configFormat = OSConfigFormatScript
	}
	if configFormat != OSConfigFormatScript && configFormat != OSConfigFormatCloudInit {
		return nil, fmt.Errorf("unsupported value for %q", OSConfigFormat)
	}
	templateName := filepath.Join("templates", strings.ToLower(configFormat)+".suse-chost.template")

	bootCmd, exists := os.LookupEnv(BootCommand)
	if !exists || bootCmd == "" {
		bootCmd = BootCommandBash
	}

	cloudInitTemplateString, err := templates.ReadFile(templateName)
	if err != nil {
		return nil, err
	}

	cloudInitTemplate, err := template.New("user-data").Parse(string(cloudInitTemplateString))
	if err != nil {
		return nil, err
	}

	return oscommontemplate.NewCloudInitGenerator(cloudInitTemplate, oscommontemplate.DefaultUnitsPath, bootCmd, func(osc *extensionsv1alpha1.OperatingSystemConfig) (map[string]interface{}, error) {
		if osc.Spec.Type != susechost.OSTypeMemoryOneCHost {
			return nil, nil
		}

		if configFormat != OSConfigFormatScript {
			return nil, fmt.Errorf("cannot render %q user-data for %q format - only %q is supported", susechost.OSTypeMemoryOneCHost, configFormat, OSConfigFormatScript)
		}

		values := map[string]interface{}{
			"MemoryTopology": "2",
			"SystemMemory":   "6x",
		}

		if osc.Spec.ProviderConfig == nil {
			return values, nil
		}

		obj := &memoryonechost.OperatingSystemConfiguration{}
		if _, _, err := decoder.Decode(osc.Spec.ProviderConfig.Raw, nil, obj); err != nil {
			return nil, fmt.Errorf("failed to decode provider config: %+v", err)
		}

		if obj.MemoryTopology != nil {
			values["MemoryTopology"] = *obj.MemoryTopology
		}
		if obj.SystemMemory != nil {
			values["SystemMemory"] = *obj.SystemMemory
		}

		return values, nil
	}), nil
}
