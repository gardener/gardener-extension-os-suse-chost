// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/gardener/gardener-extension-os-suse-chost/pkg/memoryone"
)

type actuator struct {
	client client.Client
}

// NewActuator creates a new Actuator that updates the status of the handled OperatingSystemConfig resources.
func NewActuator(mgr manager.Manager) operatingsystemconfig.Actuator {
	return &actuator{
		client: mgr.GetClient(),
	}
}

func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, osc *extensionsv1alpha1.OperatingSystemConfig) ([]byte, []extensionsv1alpha1.Unit, []extensionsv1alpha1.File, *extensionsv1alpha1.InPlaceUpdatesStatus, error) {
	switch purpose := osc.Spec.Purpose; purpose {
	case extensionsv1alpha1.OperatingSystemConfigPurposeProvision:
		userData, err := a.handleProvisionOSC(ctx, osc)
		return []byte(userData), nil, nil, nil, err

	case extensionsv1alpha1.OperatingSystemConfigPurposeReconcile:
		extensionUnits, extensionFiles, err := a.handleReconcileOSC(osc)
		return nil, extensionUnits, extensionFiles, nil, err

	default:
		return nil, nil, nil, nil, fmt.Errorf("unknown purpose: %s", purpose)
	}
}

func (a *actuator) Delete(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.OperatingSystemConfig) error {
	return nil
}

func (a *actuator) Migrate(ctx context.Context, log logr.Logger, osc *extensionsv1alpha1.OperatingSystemConfig) error {
	return a.Delete(ctx, log, osc)
}

func (a *actuator) ForceDelete(ctx context.Context, log logr.Logger, osc *extensionsv1alpha1.OperatingSystemConfig) error {
	return a.Delete(ctx, log, osc)
}

func (a *actuator) Restore(ctx context.Context, log logr.Logger, osc *extensionsv1alpha1.OperatingSystemConfig) ([]byte, []extensionsv1alpha1.Unit, []extensionsv1alpha1.File, *extensionsv1alpha1.InPlaceUpdatesStatus, error) {
	return a.Reconcile(ctx, log, osc)
}

func (a *actuator) handleProvisionOSC(ctx context.Context, osc *extensionsv1alpha1.OperatingSystemConfig) (string, error) {
	writeFilesToDiskScript, err := operatingsystemconfig.FilesToDiskScript(ctx, a.client, osc.Namespace, osc.Spec.Files)
	if err != nil {
		return "", err
	}
	writeUnitsToDiskScript := operatingsystemconfig.UnitsToDiskScript(osc.Spec.Units)

	script := `#!/bin/bash

# disable the default log rotation
mkdir -p /etc/docker/
cat <<EOF > /etc/docker/daemon.json
{
  "log-level": "warn",
  "log-driver": "json-file"
}
EOF

CONTAINERD_CONFIG_PATH=/etc/containerd/config.toml
if [[ ! -s "${CONTAINERD_CONFIG_PATH}" || $(cat ${CONTAINERD_CONFIG_PATH}) == "# See containerd-config.toml(5) for documentation." ]]; then
  mkdir -p /etc/containerd
  containerd config default > "${CONTAINERD_CONFIG_PATH}"
  chmod 0644 "${CONTAINERD_CONFIG_PATH}"
fi

if systemctl show containerd -p Conflicts | grep -q docker; then
  sed -re 's/Conflicts=(.*)(docker.service|docker)(.*)/Conflicts=\1 \3/g' -i /usr/lib/systemd/system/containerd.service
fi

mkdir -p /etc/systemd/system/containerd.service.d
cat <<EOF > /etc/systemd/system/containerd.service.d/11-exec_config.conf
[Service]
ExecStart=
ExecStart=/usr/sbin/containerd --config=${CONTAINERD_CONFIG_PATH}
EOF
chmod 0644 /etc/systemd/system/containerd.service.d/11-exec_config.conf
` + writeFilesToDiskScript + `
` + writeUnitsToDiskScript + `

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

until zypper -q install -y docker wget socat jq nfs-client; [ $? -ne 7 ]; do sleep 1; done
ln -s /usr/bin/docker /bin/docker
ln -s /bin/ip /usr/bin/ip
if [ ! -s /etc/hostname ]; then hostname > /etc/hostname; fi
systemctl daemon-reload
ln -s /usr/sbin/containerd-ctr /usr/sbin/ctr
systemctl enable containerd && systemctl restart containerd
systemctl enable docker && systemctl restart docker

# Set journald storage to persistent such that logs are written to /var/log instead of /run/log
if [[ ! -f /etc/systemd/journald.conf.d/10-use-persistent-log-storage.conf ]]; then
  mkdir -p /etc/systemd/journald.conf.d
  cat <<EOF > /etc/systemd/journald.conf.d/10-use-persistent-log-storage.conf
[Journal]
Storage=persistent
EOF
  systemctl restart systemd-journald
fi

`

	for _, unit := range osc.Spec.Units {
		script += fmt.Sprintf(`systemctl enable '%s' && systemctl restart --no-block '%s'
`, unit.Name, unit.Name)
	}

	// The provisioning script must run only once.
	script = operatingsystemconfig.WrapProvisionOSCIntoOneshotScript(script)

	if osc.Spec.Type == memoryone.OSTypeMemoryOneCHost {
		return wrapIntoMemoryOneHeaderAndFooter(osc, script)
	}

	return script, nil
}

func wrapIntoMemoryOneHeaderAndFooter(osc *extensionsv1alpha1.OperatingSystemConfig, in string) (string, error) {
	config, err := memoryone.Configuration(osc)
	if err != nil {
		return "", err
	}

	memoryTopology, systemMemory := "2", "6x"
	if config != nil && config.MemoryTopology != nil {
		memoryTopology = *config.MemoryTopology
	}
	if config != nil && config.SystemMemory != nil {
		systemMemory = *config.SystemMemory
	}

	out := `Content-Type: multipart/mixed; boundary="==BOUNDARY=="
MIME-Version: 1.0
--==BOUNDARY==
Content-Type: text/x-vsmp; section=vsmp
system_memory=` + systemMemory + `
mem_topology=` + memoryTopology + `
--==BOUNDARY==
Content-Type: text/x-shellscript
` + in + `
--==BOUNDARY==`

	return out, nil
}

func (a *actuator) handleReconcileOSC(_ *extensionsv1alpha1.OperatingSystemConfig) ([]extensionsv1alpha1.Unit, []extensionsv1alpha1.File, error) {

	// enable accepting IPv6 router advertisements so that the interface can obtain a default route
	// when IP forwarding is enabled (which it is in K8S context)
	files := []extensionsv1alpha1.File{
		{
			Path:        "/etc/sysctl.d/98-enable-ipv6-ra.conf",
			Permissions: ptr.To(uint32(0644)),
			Content: extensionsv1alpha1.FileContent{
				Inline: &extensionsv1alpha1.FileContentInline{
					Data: `# enables IPv6 router advertisements on all interfaces even when ip forwarding for IPv6 is enabled
net.ipv6.conf.all.accept_ra = 2

# specifically enable IPv6 router advertisements on the first ethernet interface (eth0 for net.ifnames=0)
net.ipv6.conf.eth0.accept_ra = 2
`,
				},
			},
		},
	}

	return nil, files, nil
}
