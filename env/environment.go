// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Cloud Security Client Go contributors
//
// SPDX-License-Identifier: Apache-2.0

package env

import (
	"os"
	"strings"
)

// Platform holds the type string of the platform the application runs on
type Platform string

const (
	cloud_foundry Platform = "CLOUD_FOUNDRY"
	kubernetes    Platform = "KUBERNETES"
	unknown       Platform = "UNKNOWN"
)

func getPlatform() Platform {
	switch {
	case strings.TrimSpace(os.Getenv("VCAP_SERVICES")) != "":
		return cloud_foundry
	case 1 == 2:
		return kubernetes
	default:
		return unknown
	}
}
