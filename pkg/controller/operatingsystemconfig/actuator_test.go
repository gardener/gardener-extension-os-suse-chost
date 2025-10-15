// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig_test

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/test"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	runtimeutils "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	memoryonev1alpha1 "github.com/gardener/gardener-extension-os-suse-chost/pkg/apis/memoryonechost/v1alpha1"
	. "github.com/gardener/gardener-extension-os-suse-chost/pkg/controller/operatingsystemconfig"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/memoryone"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/susechost"
)

var codec runtime.Codec

func init() {
	scheme := runtime.NewScheme()
	runtimeutils.Must(memoryonev1alpha1.AddToScheme(scheme))
	codec = serializer.NewCodecFactory(scheme, serializer.EnableStrict).LegacyCodec(memoryonev1alpha1.SchemeGroupVersion)
}

var _ = Describe("Actuator", func() {
	var (
		ctx        = context.TODO()
		log        = logr.Discard()
		fakeClient client.Client
		mgr        manager.Manager

		osc      *extensionsv1alpha1.OperatingSystemConfig
		actuator operatingsystemconfig.Actuator
	)

	BeforeEach(func() {
		fakeClient = fakeclient.NewClientBuilder().Build()
		mgr = test.FakeManager{Client: fakeClient}
		actuator = NewActuator(mgr)

		osc = &extensionsv1alpha1.OperatingSystemConfig{
			Spec: extensionsv1alpha1.OperatingSystemConfigSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type: susechost.OSTypeSuSECHost,
				},
				Purpose: extensionsv1alpha1.OperatingSystemConfigPurposeProvision,
				Units:   []extensionsv1alpha1.Unit{{Name: "some-unit", Content: ptr.To("foo")}},
				Files:   []extensionsv1alpha1.File{{Path: "/some/file", Content: extensionsv1alpha1.FileContent{Inline: &extensionsv1alpha1.FileContentInline{Data: "bar"}}}},
			},
		}
	})

	When("purpose is 'provision'", func() {
		expectedUserData := `#!/bin/bash
if [ -f "/var/lib/osc/provision-osc-applied" ]; then
  echo "Provision OSC already applied, exiting..."
  exit 0
fi

CONTAINERD_CONFIG_PATH=/etc/containerd/config.toml
if [[ ! -s "${CONTAINERD_CONFIG_PATH}" || $(cat ${CONTAINERD_CONFIG_PATH}) == "# See containerd-config.toml(5) for documentation." ]]; then
  mkdir -p /etc/containerd
  containerd config default > "${CONTAINERD_CONFIG_PATH}"
  chmod 0644 "${CONTAINERD_CONFIG_PATH}"
fi

# refer to https://github.com/gardener/gardener-extension-os-suse-chost/tree/master/docs/systemd-units.md
if systemctl show containerd -p Conflicts | grep -q docker; then
  cp /usr/lib/systemd/system/containerd.service /etc/systemd/system/containerd.service
  sed -re 's/Conflicts=(.*)(docker.service|docker)(.*)/Conflicts=\1 \3/g' -i /etc/systemd/system/containerd.service
fi

mkdir -p /etc/systemd/system/containerd.service.d
cat <<EOF > /etc/systemd/system/containerd.service.d/11-exec_config.conf
[Service]
ExecStart=
ExecStart=/usr/sbin/containerd --config=${CONTAINERD_CONFIG_PATH}
EOF
chmod 0644 /etc/systemd/system/containerd.service.d/11-exec_config.conf

mkdir -p "/some"

cat << EOF | base64 -d > "/some/file"
YmFy
EOF


cat << EOF | base64 -d > "/etc/systemd/system/some-unit"
Zm9v
EOF

until zypper -q install -y wget socat jq nfs-client; [ $? -ne 7 ]; do sleep 1; done
ln -s /bin/ip /usr/bin/ip
if [ ! -s /etc/hostname ]; then hostname > /etc/hostname; fi
systemctl daemon-reload
ln -s /usr/sbin/containerd-ctr /usr/sbin/ctr
systemctl enable containerd && systemctl restart containerd

systemctl disable docker && systemctl stop docker || echo "No docker service to disable or stop"

# Set journald storage to persistent such that logs are written to /var/log instead of /run/log
if [[ ! -f /etc/systemd/journald.conf.d/10-use-persistent-log-storage.conf ]]; then
  mkdir -p /etc/systemd/journald.conf.d
  cat <<EOF > /etc/systemd/journald.conf.d/10-use-persistent-log-storage.conf
[Journal]
Storage=persistent
EOF
  systemctl restart systemd-journald
fi

systemctl enable 'some-unit' && systemctl restart --no-block 'some-unit'


mkdir -p /var/lib/osc
touch /var/lib/osc/provision-osc-applied
`

		When("OS type is 'suse-chost'", func() {
			Describe("#Reconcile", func() {
				It("should not return an error", func() {
					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					Expect(string(userData)).To(Equal(expectedUserData))
					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})
			})
		})

		When("OS type is 'memoryone-chost'", func() {
			var (
				memoryOneConfiguration memoryonev1alpha1.OperatingSystemConfiguration
			)

			BeforeEach(func() {
				memoryOneConfiguration = memoryonev1alpha1.OperatingSystemConfiguration{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "memoryone-chost.os.extensions.gardener.cloud/v1alpha1",
						Kind:       "OperatingSystemConfiguration",
					},
				}

				osc.Spec.Type = memoryone.OSTypeMemoryOneCHost
			})

			When("Legacy fields are used", func() {
				It("should use default values for the system_memory and mem_topology", func() {
					osc.Spec.ProviderConfig = nil

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					vSmpConfig, decodedUserData := decodeVsmpUserData(string(userData))

					Expect(vSmpConfig).To(BeEquivalentTo(map[string]string{
						"mem_topology":  "2",
						"system_memory": "6x",
					}))
					Expect(decodedUserData).To(Equal(expectedUserData))
					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})

				It("should use custom values for system_memory and mem_topology", func() {
					memoryOneConfiguration.MemoryTopology = ptr.To("4")
					memoryOneConfiguration.SystemMemory = ptr.To("8x")
					Expect(encodeMemoryOneConfigurationIntoOsc(codec, osc, &memoryOneConfiguration)).To(Succeed())

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					vSmpConfig, decodedUserData := decodeVsmpUserData(string(userData))

					Expect(vSmpConfig).To(BeEquivalentTo(map[string]string{
						"mem_topology":  "4",
						"system_memory": "8x",
					}))
					Expect(decodedUserData).To(Equal(expectedUserData))

					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})

				It("should allow injecting additional key-value pairs by semicola", func() {
					memoryOneConfiguration.MemoryTopology = ptr.To("4; foo=bar")
					memoryOneConfiguration.SystemMemory = ptr.To("8x")
					Expect(encodeMemoryOneConfigurationIntoOsc(codec, osc, &memoryOneConfiguration)).To(Succeed())

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					vSmpConfig, decodedUserData := decodeVsmpUserData(string(userData))

					Expect(vSmpConfig).To(BeEquivalentTo(map[string]string{
						"mem_topology":  "4; foo=bar",
						"system_memory": "8x",
					}))
					Expect(decodedUserData).To(Equal(expectedUserData))

					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})
			})

			When("MemoryOne configuration map is used", func() {
				It("Should include arbitrary configuration values in vSMP config", func() {
					memoryOneConfiguration.VsmpConfiguration = map[string]string{
						"foo": "bar",
						"abc": "xyz",
					}

					Expect(encodeMemoryOneConfigurationIntoOsc(codec, osc, &memoryOneConfiguration)).To(Succeed())

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					vSmpConfig, decodedUserData := decodeVsmpUserData(string(userData))

					Expect(vSmpConfig).To(BeEquivalentTo(map[string]string{
						"mem_topology":  "2",
						"system_memory": "6x",
						"foo":           "bar",
						"abc":           "xyz",
					}))
					Expect(decodedUserData).To(Equal(expectedUserData))

					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})

				It("Should not allow injecting additional key-value pairs by semicola", func() {
					memoryOneConfiguration.VsmpConfiguration = map[string]string{
						"foo": "bar; foobar: barfoo",
						"abc": "xyz",
					}

					Expect(encodeMemoryOneConfigurationIntoOsc(codec, osc, &memoryOneConfiguration)).To(Succeed())

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					vSmpConfig, decodedUserData := decodeVsmpUserData(string(userData))

					Expect(vSmpConfig).To(BeEquivalentTo(map[string]string{
						"mem_topology":  "2",
						"system_memory": "6x",
						"foo":           "bar",
						"abc":           "xyz",
					}))
					Expect(decodedUserData).To(Equal(expectedUserData))

					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})

				It("Should allow quoted values", func() {
					memoryOneConfiguration.VsmpConfiguration = map[string]string{
						"quoted": "\"12:34:56:78:90:ab:cd:ef\"",
					}

					Expect(encodeMemoryOneConfigurationIntoOsc(codec, osc, &memoryOneConfiguration)).To(Succeed())

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					vSmpConfig, decodedUserData := decodeVsmpUserData(string(userData))

					Expect(vSmpConfig).To(BeEquivalentTo(map[string]string{
						"mem_topology":  "2",
						"system_memory": "6x",
						"quoted":        "\"12:34:56:78:90:ab:cd:ef\"",
					}))
					Expect(decodedUserData).To(Equal(expectedUserData))

					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})

				It("Should give priority to legacy values", func() {
					memoryOneConfiguration.MemoryTopology = ptr.To("3")
					memoryOneConfiguration.SystemMemory = ptr.To("7x")
					memoryOneConfiguration.VsmpConfiguration = map[string]string{
						"mem_topology":  "5",
						"system_memory": "13x",
					}

					Expect(encodeMemoryOneConfigurationIntoOsc(codec, osc, &memoryOneConfiguration)).To(Succeed())

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					vSmpConfig, decodedUserData := decodeVsmpUserData(string(userData))

					Expect(vSmpConfig).To(BeEquivalentTo(map[string]string{
						"mem_topology":  "3",
						"system_memory": "7x",
					}))
					Expect(decodedUserData).To(Equal(expectedUserData))

					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})
			})
		})
	})

	When("purpose is 'reconcile'", func() {
		BeforeEach(func() {
			osc.Spec.Purpose = extensionsv1alpha1.OperatingSystemConfigPurposeReconcile
		})

		Describe("#Reconcile", func() {
			It("should not return an error", func() {
				userData, extensionUnits, _, _, err := actuator.Reconcile(ctx, log, osc)
				Expect(err).NotTo(HaveOccurred())

				Expect(userData).To(BeEmpty())
				Expect(extensionUnits).To(BeEmpty())
			})

			It("should deploy a sysctl file to configure IPv6 router advertisements", func() {
				_, _, extensionFiles, _, err := actuator.Reconcile(ctx, log, osc)
				Expect(err).NotTo(HaveOccurred())

				sysctl_content := `# enables IPv6 router advertisements on all interfaces even when ip forwarding for IPv6 is enabled
net.ipv6.conf.all.accept_ra = 2

# specifically enable IPv6 router advertisements on the first ethernet interface (eth0 for net.ifnames=0)
net.ipv6.conf.eth0.accept_ra = 2
`

				Expect(extensionFiles).To(HaveLen(1))
				Expect(extensionFiles[0].Path).To(Equal("/etc/sysctl.d/98-enable-ipv6-ra.conf"))
				Expect(extensionFiles[0].Permissions).To(Equal(ptr.To(uint32(0644))))
				Expect(extensionFiles[0].Content.Inline.Data).To(Equal(sysctl_content))
			})
		})
	})
})

type multiPart struct {
	contentType string
	params      map[string]string
	content     string
}

func readMimeMultiParts(s string) []multiPart {
	GinkgoHelper()
	const (
		contentTypeIdentifier = "Content-Type"
		boundary              = "==BOUNDARY=="
	)

	var parts []multiPart

	mr := multipart.NewReader(strings.NewReader(s), boundary)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		Expect(err).ShouldNot(HaveOccurred())

		c, err := io.ReadAll(p)
		Expect(err).ShouldNot(HaveOccurred())

		mediaType, params, err := mime.ParseMediaType(p.Header.Get(contentTypeIdentifier))
		Expect(err).ShouldNot(HaveOccurred())

		parts = append(parts, multiPart{
			contentType: mediaType,
			params:      params,
			content:     string(c),
		})
	}
	return parts
}

func extractVsmpConfiguration(p multiPart) map[string]string {
	GinkgoHelper()
	Expect(p.contentType).To(Equal("text/x-vsmp"))
	Expect(p.params).To(HaveLen(1))
	Expect(p.params).To(HaveKeyWithValue("section", "vsmp"))

	lines := strings.Split(p.content, "\n")

	var config = make(map[string]string, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		Expect(found).To(BeTrue())
		config[key] = value
	}

	return config
}

func extractUserdata(p multiPart) string {
	GinkgoHelper()
	Expect(p.contentType).To(Equal("text/x-shellscript"))
	Expect(p.params).To(BeEmpty())
	return p.content
}

func decodeVsmpUserData(s string) (map[string]string, string) {
	GinkgoHelper()
	prefix := `Content-Type: multipart/mixed; boundary="==BOUNDARY=="
MIME-Version: 1.0`

	Expect(strings.HasPrefix(s, prefix)).To(BeTrue())
	parts := readMimeMultiParts(s)
	Expect(parts).To(HaveLen(2))
	vsmpConfig := extractVsmpConfiguration(parts[0])
	userData := extractUserdata(parts[1])
	return vsmpConfig, userData
}

func encodeMemoryOneConfigurationIntoOsc(codec runtime.Codec, osc *extensionsv1alpha1.OperatingSystemConfig, moc *memoryonev1alpha1.OperatingSystemConfiguration) error {
	GinkgoHelper()
	encoded, err := runtime.Encode(codec, moc)
	if err != nil {
		return err
	}
	osc.Spec.ProviderConfig = &runtime.RawExtension{
		Raw: encoded,
	}
	return nil
}
