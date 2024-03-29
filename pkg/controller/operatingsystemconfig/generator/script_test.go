// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package generator_test

import (
	oscommongenerator "github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/generator"
	"github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/generator/test"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/controller/operatingsystemconfig/generator"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/controller/operatingsystemconfig/generator/testfiles/script"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/memoryone"
)

var logger = logr.Discard()

var _ = Describe("Script Generator Test", func() {
	gen := generator.NewCloudInitGenerator()

	Describe("Conformance Tests Script", test.DescribeTest(gen, script.Files))

	Context("memory one", func() {
		var (
			onlyOwnerPerm = int32(0600)
			osConfig      = &oscommongenerator.OperatingSystemConfig{
				Object: &extensionsv1alpha1.OperatingSystemConfig{
					Spec: extensionsv1alpha1.OperatingSystemConfigSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type: memoryone.OSTypeMemoryOneCHost,
							ProviderConfig: &runtime.RawExtension{
								Raw: encode(&memoryonechost.OperatingSystemConfiguration{
									MemoryTopology: ptr.To("3"),
									SystemMemory:   ptr.To("7x"),
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
					{
						Name: "cloud-config-downloader.service",
					},
				},
				Bootstrap: true,
			}
			expectedCloudInitBootstrap []byte
			expectedCloudInitReconcile []byte
			err                        error
		)

		BeforeEach(func() {
			expectedCloudInitBootstrap, err = script.Files.ReadFile("script.memoryone-chost-bootstrap")
			Expect(err).NotTo(HaveOccurred())
			expectedCloudInitReconcile, err = script.Files.ReadFile("script.memoryone-chost-reconcile")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should render correctly bootstrap", func() {
			cloudInit, _, err := gen.Generate(logger, osConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(cloudInit).To(Equal(expectedCloudInitBootstrap))
		})

		It("should render correctly reconcile", func() {
			osConfig.Bootstrap = false
			cloudInit, _, err := gen.Generate(logger, osConfig)

			Expect(err).NotTo(HaveOccurred())
			Expect(cloudInit).To(Equal(expectedCloudInitReconcile))
		})
	})
})
