// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"database/sql"
	"strings"
)

const channelArchTypeSql = `SELECT at.label
FROM rhnchannel c
JOIN rhnchannelarch ca ON ca.id = c.channel_arch_id
JOIN rhnarchtype at ON at.id = ca.arch_type_id
WHERE c.label = $1`

// LookupChannelArchType returns the arch type label (rpm or deb) of a channel.
func LookupChannelArchType(db *sql.DB, label string) (string, error) {
	var archType string
	if err := db.QueryRow(channelArchTypeSql, label).Scan(&archType); err != nil {
		return "", err
	}
	return archType, nil
}

// NormalizeArchType trims and lower-cases an arch type label.
func NormalizeArchType(archType string) string {
	return strings.ToLower(strings.TrimSpace(archType))
}

// IsDebArchType reports whether the arch type label denotes a Debian channel.
func IsDebArchType(archType string) bool {
	return NormalizeArchType(archType) == "deb"
}
