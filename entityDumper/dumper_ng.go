// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

// DumpAllEntitiesNG exports channels using repository metadata and package files.
func DumpAllEntitiesNG(options DumperOptionsNG) {
	dumpEntities(options.DumperOptions, processAndInsertChannelsNG)
}
