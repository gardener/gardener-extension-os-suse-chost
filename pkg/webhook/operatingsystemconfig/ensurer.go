// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package operatingsystemconfig

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"k8s.io/utils/ptr"
)

var (
	cgroupv2cHost15sp5 *semver.Version
)

func init() {
	// cHost 15 SP5 changed to cgroup v2 with build 20241011
	// see https://publiccloudimagechangeinfo.suse.com/amazon/suse-sles-15-sp5-chost-byos-v20241011-hvm-ssd-arm64/image_changes.html
	cgroupv2cHost15sp5 = semver.MustParse("15.5.20241011")
}

const (
	cgroupDriverSystemd  = "systemd"
	cgroupDriverCgroupfs = "cgroupfs"
)

func ensureKubeletConfiguration(logger logr.Logger, cHostVersion semver.Version, new *kubeletconfigv1beta1.KubeletConfiguration) {
	cgroupDriver := determineChostCgroupDriver(cHostVersion)

	logger.Info(fmt.Sprintf("Ensuring Kubelet cgroup driver %s for cHost %v", cgroupDriver, cHostVersion))
	new.CgroupDriver = cgroupDriver
}

func ensureCRIConfig(logger logr.Logger, cHostVersion semver.Version, new *extensionsv1alpha1.CRIConfig) {
	cgroupDriver := determineChostCgroupDriver(cHostVersion)

	logger.Info(fmt.Sprintf("Ensuring containerd cgroup driver %s for cHost %v", cgroupDriver, cHostVersion))

	if cgroupDriver == cgroupDriverSystemd {
		new.CgroupDriver = ptr.To(extensionsv1alpha1.CgroupDriverSystemd)
	} else {
		new.CgroupDriver = ptr.To(extensionsv1alpha1.CgroupDriverCgroupfs)
	}
}

func determineChostCgroupDriver(chostVersion semver.Version) string {
	if chostVersion.Major() > 15 {
		return cgroupDriverSystemd
	}

	if chostVersion.Major() == 15 {

		if chostVersion.Minor() > 5 {
			return cgroupDriverSystemd
		}

		if chostVersion.Minor() == 5 {
			if chostVersion.GreaterThanEqual(cgroupv2cHost15sp5) {
				return cgroupDriverSystemd
			} else {
				return cgroupDriverCgroupfs
			}
		}

		if chostVersion.Minor() < 5 {
			return cgroupDriverCgroupfs
		}
	}

	return cgroupDriverCgroupfs
}
