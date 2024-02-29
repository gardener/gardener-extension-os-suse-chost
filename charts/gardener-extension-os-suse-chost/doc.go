// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

//go:generate sh -c "../../vendor/github.com/gardener/gardener/hack/generate-controller-registration.sh os-suse-chost . $(cat ../../VERSION) ../../example/controller-registration.yaml OperatingSystemConfig:suse-chost OperatingSystemConfig:memoryone-chost"

// Package chart enables go:generate support for generating the correct controller registration.
package chart
