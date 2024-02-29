// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig"
	oscommonactuator "github.com/gardener/gardener/extensions/pkg/controller/operatingsystemconfig/oscommon/actuator"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/gardener/gardener-extension-os-suse-chost/pkg/controller/operatingsystemconfig/generator"
	"github.com/gardener/gardener-extension-os-suse-chost/pkg/memoryone"
)

type actuator struct {
	client               client.Client
	useGardenerNodeAgent bool
}

// NewActuator creates a new Actuator that updates the status of the handled OperatingSystemConfig resources.
func NewActuator(mgr manager.Manager, useGardenerNodeAgent bool) operatingsystemconfig.Actuator {
	return &actuator{
		client:               mgr.GetClient(),
		useGardenerNodeAgent: useGardenerNodeAgent,
	}
}

func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, osc *extensionsv1alpha1.OperatingSystemConfig) ([]byte, *string, []string, []string, []extensionsv1alpha1.Unit, []extensionsv1alpha1.File, error) {
	cloudConfig, command, err := oscommonactuator.CloudConfigFromOperatingSystemConfig(ctx, log, a.client, osc, generator.NewCloudInitGenerator())
	if err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("could not generate cloud config: %w", err)
	}

	switch purpose := osc.Spec.Purpose; purpose {
	case extensionsv1alpha1.OperatingSystemConfigPurposeProvision:
		if !a.useGardenerNodeAgent {
			return cloudConfig, command, oscommonactuator.OperatingSystemConfigUnitNames(osc), oscommonactuator.OperatingSystemConfigFilePaths(osc), nil, nil, nil
		}
		userData, err := a.handleProvisionOSC(ctx, osc)
		return []byte(userData), nil, nil, nil, nil, nil, err

	case extensionsv1alpha1.OperatingSystemConfigPurposeReconcile:
		extensionUnits, extensionFiles, err := a.handleReconcileOSC(osc)
		return cloudConfig, command, oscommonactuator.OperatingSystemConfigUnitNames(osc), oscommonactuator.OperatingSystemConfigFilePaths(osc), extensionUnits, extensionFiles, err

	default:
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("unknown purpose: %s", purpose)
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

func (a *actuator) Restore(ctx context.Context, log logr.Logger, osc *extensionsv1alpha1.OperatingSystemConfig) ([]byte, *string, []string, []string, []extensionsv1alpha1.Unit, []extensionsv1alpha1.File, error) {
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
if [[ -d /sys/class/net/eth0 ]]
then
  ip link set dev eth0 mtu 1460
  grep -q '^MTU' /etc/sysconfig/network/ifcfg-eth0 && sed -i 's/^MTU.*/MTU=1460/' /etc/sysconfig/network/ifcfg-eth0 || echo 'MTU=1460' >> /etc/sysconfig/network/ifcfg-eth0
  wicked ifreload eth0
fi

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
	// os-suse-chost does not add any additional units or additional files
	return nil, nil, nil
}
