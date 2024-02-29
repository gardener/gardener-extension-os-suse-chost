// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"embed"
	"fmt"
	"path/filepath"

	oscommontemplate "github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/template"
	ostemplate "github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/template"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	runtimeutils "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost"
	memoryonechostinstall "github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost/install"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/memoryone"
)

var (
	bootCmd            = "/bin/bash %s"
	cloudInitGenerator *ostemplate.CloudInitGenerator
	decoder            runtime.Decoder
)

//go:embed templates/*
var templates embed.FS

func init() {
	scheme := runtime.NewScheme()
	runtimeutils.Must(memoryonechostinstall.AddToScheme(scheme))
	decoder = serializer.NewCodecFactory(scheme).UniversalDecoder()

	scriptTemplateString, err := templates.ReadFile(filepath.Join("templates", "script.suse-chost.template"))
	runtimeutils.Must(err)

	cloudInitTemplate, err := ostemplate.NewTemplate("script").Parse(string(scriptTemplateString))
	runtimeutils.Must(err)

	cloudInitGenerator = oscommontemplate.NewCloudInitGenerator(cloudInitTemplate, oscommontemplate.DefaultUnitsPath, bootCmd, func(osc *extensionsv1alpha1.OperatingSystemConfig) (map[string]interface{}, error) {
		if osc.Spec.Type != memoryone.OSTypeMemoryOneCHost {
			return nil, nil
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
	})
}

// NewCloudInitGenerator creates a new Generator using the template file for suse-chost
func NewCloudInitGenerator() *oscommontemplate.CloudInitGenerator {
	return cloudInitGenerator
}
