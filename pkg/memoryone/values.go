// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package memoryone

import (
	"fmt"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	runtimeutils "k8s.io/apimachinery/pkg/util/runtime"

	memoryonechost "github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost/v1alpha1"
)

var decoder runtime.Decoder

func init() {
	scheme := runtime.NewScheme()
	runtimeutils.Must(memoryonechost.AddToScheme(scheme))
	decoder = serializer.NewCodecFactory(scheme).UniversalDecoder()
}

func Configuration(osc *extensionsv1alpha1.OperatingSystemConfig) (*memoryonechost.OperatingSystemConfiguration, error) {
	if osc.Spec.ProviderConfig == nil {
		return nil, nil
	}

	obj := &memoryonechost.OperatingSystemConfiguration{}
	if _, _, err := decoder.Decode(osc.Spec.ProviderConfig.Raw, nil, obj); err != nil {
		return nil, fmt.Errorf("failed to decode provider config: %+v", err)
	}

	return obj, nil
}
