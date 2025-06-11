// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig_test

import (
	"context"

	"github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/test"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	. "github.com/gardener/gardener-extension-os-suse-chost/pkg/controller/operatingsystemconfig"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/memoryone"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/susechost"
)

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

# Some versions of SuSE CHost comes with a predefined docker unit - enabled but not started.
# In case of reboot, the docker unit is started and prevents the containerd unit from starting.
# Due to this reason, update the containerd unit to do not conflict with the docker unit.
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

# mitigate https://github.com/systemd/systemd/issues/7082
# ref https://github.com/coreos/bugs/issues/2193#issuecomment-337767555
SYSTEMD_VERSION=$(rpm -q --qf %{VERSION} systemd | grep -Po '^[1-9]\d*')
SUSE_VARIANT_VERSION=$(grep -oP '(?<=^VARIANT_VERSION=).+' /etc/os-release | tr -d '"')
SUSE_SP_ID=$(grep -oP '(?<=^VERSION_ID=).+' /etc/os-release | tr -d '"' | cut -d '.' -f 2)

if [[ $SYSMTED_VERSION -lt 236 && -n $SUSE_SP_ID && $SUSE_SP_ID -lt 3 && -n $SUSE_VARIANT_VERSION && $SUSE_VARIANT_VERSION -lt 20210722 ]]; then
  mkdir -p /etc/systemd/system/systemd-hostnamed.service.d/
  cat <<EOF > /etc/systemd/system/systemd-hostnamed.service.d/10-protect-system.conf
[Service]
ProtectSystem=full
EOF
  systemctl daemon-reload
fi

until zypper -q install -y wget socat jq nfs-client; [ $? -ne 7 ]; do sleep 1; done
ln -s /bin/ip /usr/bin/ip
if [ ! -s /etc/hostname ]; then hostname > /etc/hostname; fi
systemctl daemon-reload
ln -s /usr/sbin/containerd-ctr /usr/sbin/ctr
systemctl enable containerd && systemctl restart containerd

# Some versions of SuSE CHost comes with a predefined docker unit - enabled but not started.
# Disable the docker unit to prevent a reboot from starting it.
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
			BeforeEach(func() {
				osc.Spec.Type = memoryone.OSTypeMemoryOneCHost
				osc.Spec.ProviderConfig = &runtime.RawExtension{Raw: []byte(`apiVersion: memoryone-chost.os.extensions.gardener.cloud/v1alpha1
kind: OperatingSystemConfiguration
memoryTopology: "4"
systemMemory: "8x"`)}
			})

			Describe("#Reconcile", func() {
				It("should not return an error", func() {
					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					Expect(string(userData)).To(Equal(`Content-Type: multipart/mixed; boundary="==BOUNDARY=="
MIME-Version: 1.0
--==BOUNDARY==
Content-Type: text/x-vsmp; section=vsmp
system_memory=8x
mem_topology=4
--==BOUNDARY==
Content-Type: text/x-shellscript
` + expectedUserData + `
--==BOUNDARY==`))
					Expect(extensionUnits).To(BeEmpty())
					Expect(extensionFiles).To(BeEmpty())
					Expect(inplaceUpdateStatus).To(BeNil())
				})

				It("should use default values for the system_memory and mem_topology", func() {
					osc.Spec.ProviderConfig = nil

					userData, extensionUnits, extensionFiles, inplaceUpdateStatus, err := actuator.Reconcile(ctx, log, osc)
					Expect(err).NotTo(HaveOccurred())

					Expect(string(userData)).To(Equal(`Content-Type: multipart/mixed; boundary="==BOUNDARY=="
MIME-Version: 1.0
--==BOUNDARY==
Content-Type: text/x-vsmp; section=vsmp
system_memory=6x
mem_topology=2
--==BOUNDARY==
Content-Type: text/x-shellscript
` + expectedUserData + `
--==BOUNDARY==`))
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

				Expect(len(extensionFiles)).To(Equal(1))
				Expect(extensionFiles[0].Path).To(Equal("/etc/sysctl.d/98-enable-ipv6-ra.conf"))
				Expect(extensionFiles[0].Permissions).To(Equal(ptr.To(uint32(0644))))
				Expect(extensionFiles[0].Content.Inline.Data).To(Equal(sysctl_content))
			})
		})
	})
})
