// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package script

import (
	"embed"
)

// Files contains the contents of the script testfiles directory
//
//go:embed cloud-init script*
var Files embed.FS
