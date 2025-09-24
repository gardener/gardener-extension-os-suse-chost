// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	corev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/component/extensions/operatingsystemconfig/original/components/kubelet"
	oscutils "github.com/gardener/gardener/pkg/component/extensions/operatingsystemconfig/utils"
	mockclient "github.com/gardener/gardener/third_party/mock/controller-runtime/client"
	mockmanager "github.com/gardener/gardener/third_party/mock/controller-runtime/manager"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener-extension-os-suse-chost/pkg/webhook/operatingsystemconfig"
)

func TestOperatingSystemConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook OperatingSystemConfig Suite")
}

const (
	shootNamespace = "shoot--foo--bar"

	workerPoolName1   = "worker-foo-1"
	workerPoolOSName1 = "suse-chost"

	kubeletCgroupDriverCgroupFs = "cgroupfs"
	kubeletCgroupDriverSystemd  = "systemd"
)

var (
	kubeletConfigCodec = kubelet.NewConfigCodec(fciCodec)
	fciCodec           = oscutils.NewFileContentInlineCodec()
)

var _ = Describe("Mutate", func() {
	var (
		ctrl *gomock.Controller
		c    *mockclient.MockClient
		mgr  *mockmanager.MockManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		c = mockclient.NewMockClient(ctrl)
		mgr = mockmanager.NewMockManager(ctrl)
		mgr.EXPECT().GetClient().Return(c)
	})

	Describe("Reconcile OSC", func() {
		var (
			mutator extensionswebhook.Mutator
			cluster *controller.Cluster

			logger logr.Logger
			ctx    context.Context

			osc extensionsv1alpha1.OperatingSystemConfig
		)

		BeforeEach(func() {
			logger = logr.Discard()
			kubeletConfigCodec = kubelet.NewConfigCodec(fciCodec)
			mutator = operatingsystemconfig.NewMutator(mgr, kubeletConfigCodec, fciCodec, logger)

			c.EXPECT().Get(ctx, client.ObjectKey{Name: shootNamespace}, gomock.AssignableToTypeOf(&extensionsv1alpha1.Cluster{})).
				DoAndReturn(
					func(_ context.Context, _ types.NamespacedName, obj *extensionsv1alpha1.Cluster, _ ...client.GetOption) error {
						shootJSON, err := json.Marshal(cluster.Shoot)
						Expect(err).NotTo(HaveOccurred())
						*obj = extensionsv1alpha1.Cluster{
							ObjectMeta: cluster.ObjectMeta,
							Spec: extensionsv1alpha1.ClusterSpec{
								Shoot: runtime.RawExtension{Raw: shootJSON},
							},
						}
						return nil
					}).AnyTimes()

			cluster = &controller.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: shootNamespace,
				},
				Shoot: &corev1beta1.Shoot{
					Spec: corev1beta1.ShootSpec{
						Provider: corev1beta1.Provider{
							Workers: []corev1beta1.Worker{
								{
									Name: workerPoolName1,
									Machine: corev1beta1.Machine{
										Image: &corev1beta1.ShootMachineImage{
											Name: workerPoolOSName1,
										},
									},
								},
							},
						},
					},
				},
			}

			kubeletConfigTemplate := kubeletconfigv1beta1.KubeletConfiguration{
				CgroupDriver: "cgroupfs",
			}

			files, err := filesWithKkubletConfig(&kubeletConfigTemplate)
			Expect(err).ToNot(HaveOccurred())

			osc = extensionsv1alpha1.OperatingSystemConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: shootNamespace,
					Labels: map[string]string{
						v1beta1constants.LabelWorkerPool: workerPoolName1,
					},
				},
				Spec: extensionsv1alpha1.OperatingSystemConfigSpec{
					Purpose: extensionsv1alpha1.OperatingSystemConfigPurposeReconcile,
					CRIConfig: &extensionsv1alpha1.CRIConfig{
						CgroupDriver: ptr.To(extensionsv1alpha1.CgroupDriverCgroupfs),
					},
					Files: files,
				},
			}
		})

		DescribeTable("Setting the cgroup driver depending on cHost version",
			func(cHostVersion string, criCgroupDriver extensionsv1alpha1.CgroupDriverName, kubeletCgroupDriver string) {
				cluster.Shoot.Spec.Provider.Workers[0].Machine.Image.Version = &cHostVersion

				err := mutator.Mutate(ctx, &osc, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(osc.Spec.CRIConfig.CgroupDriver).To(Equal(ptr.To(criCgroupDriver)))

				newKubeletConfig, err := extractKubeletConfig(osc.Spec.Files)
				Expect(err).ToNot(HaveOccurred())
				Expect(newKubeletConfig.CgroupDriver).To(Equal(kubeletCgroupDriver))
			},

			Entry("on cHost 15 SP4", "15.4.20240102", extensionsv1alpha1.CgroupDriverCgroupfs, kubeletCgroupDriverCgroupFs),
			Entry("on cHost 15 SP5", "15.5.20240529", extensionsv1alpha1.CgroupDriverCgroupfs, kubeletCgroupDriverCgroupFs),
			Entry("on cHost 15 SP6 before build timestamp 20250918", "15.6.20250819", extensionsv1alpha1.CgroupDriverCgroupfs, kubeletCgroupDriverCgroupFs),
			Entry("on cHost 15 SP6 starting with build timestamp 20251201", "15.6.20260101", extensionsv1alpha1.CgroupDriverSystemd, kubeletCgroupDriverSystemd),
			Entry("on cHost 15 SP7 before build timestamp 20250625", "15.7.20250625", extensionsv1alpha1.CgroupDriverCgroupfs, kubeletCgroupDriverCgroupFs),
			Entry("on cHost 15 SP7 starting with build timestamp 20251201", "15.7.20260101", extensionsv1alpha1.CgroupDriverSystemd, kubeletCgroupDriverSystemd),
			Entry("on cHost 15 greater than SP7", "15.8.20250102", extensionsv1alpha1.CgroupDriverSystemd, kubeletCgroupDriverSystemd),
		)
	})
})

func extractKubeletConfig(oscFiles []extensionsv1alpha1.File) (*kubeletconfigv1beta1.KubeletConfiguration, error) {
	var kubeletConfigFCI *extensionsv1alpha1.FileContentInline
	for _, f := range oscFiles {
		if f.Path != v1beta1constants.OperatingSystemConfigFilePathKubeletConfig {
			continue
		}

		kubeletConfigFCI = f.Content.Inline
	}

	if kubeletConfigFCI == nil {
		return nil, fmt.Errorf("no kubeletconfig found inline")
	}

	kubeletConfig, err := kubeletConfigCodec.Decode(kubeletConfigFCI)
	if err != nil {
		return nil, err
	}

	return kubeletConfig, nil
}

func filesWithKkubletConfig(kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration) ([]extensionsv1alpha1.File, error) {
	kubeletConfigFci, err := kubeletConfigCodec.Encode(kubeletConfig, "b64")
	if err != nil {
		return nil, err
	}

	return []extensionsv1alpha1.File{
		{
			Path:        v1beta1constants.OperatingSystemConfigFilePathKubeletConfig,
			Permissions: ptr.To(uint32(0644)),
			Content: extensionsv1alpha1.FileContent{
				Inline: kubeletConfigFci,
			},
		},
	}, nil
}
