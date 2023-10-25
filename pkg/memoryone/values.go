// Copyright (c) 2022 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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
