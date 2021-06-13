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

package generator_test

import (
	"os"

	"github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/generator"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/generator/testfiles/cloudinit"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/susechost"

	oscommongenerator "github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/generator"
	"github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/generator/test"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
)

var _ = Describe("Cloud-init Generator Test", func() {
	os.Setenv(generator.BootCommand, "cloud-init-command")
	os.Setenv(generator.OSConfigFormat, generator.OSConfigFormatCloudInit)
	gen, err := generator.NewCloudInitGenerator()

	It("should not fail creating generator", func() {
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Conformance Tests Cloud Init", test.DescribeTest(gen, cloudinit.Files))

	Context("memory one", func() {
		var (
			onlyOwnerPerm = int32(0600)
			osConfig      = &oscommongenerator.OperatingSystemConfig{
				Object: &extensionsv1alpha1.OperatingSystemConfig{
					Spec: extensionsv1alpha1.OperatingSystemConfigSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type: susechost.OSTypeMemoryOneCHost,
							ProviderConfig: &runtime.RawExtension{
								Raw: encode(&memoryonechost.OperatingSystemConfiguration{
									MemoryTopology: pointer.StringPtr("3"),
									SystemMemory:   pointer.StringPtr("7x"),
								}),
							},
						},
						Purpose: extensionsv1alpha1.OperatingSystemConfigPurposeProvision,
					},
				},
				Files: []*oscommongenerator.File{
					{
						Path:        "/foo",
						Content:     []byte("bar"),
						Permissions: &onlyOwnerPerm,
					},
				},

				Units: []*oscommongenerator.Unit{
					{
						Name:    "docker.service",
						Content: []byte("unit"),
						DropIns: []*oscommongenerator.DropIn{
							{
								Name:    "10-docker-opts.conf",
								Content: []byte("override"),
							},
						},
					},
				},
				Bootstrap: true,
			}
		)

		It("should fail to render because format type not supported", func() {
			_, _, err := gen.Generate(osConfig)
			Expect(err).To(HaveOccurred())
		})
	})
})
