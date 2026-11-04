// SPDX-FileCopyrightText: 2023 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package packageDumper

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/inter-server-sync/dumper"
	"github.com/uyuni-project/inter-server-sync/schemareader"
	"github.com/uyuni-project/inter-server-sync/sqlUtil"
)

func DumpPackageFiles(db *sql.DB, schemaMetadata map[string]schemareader.Table, data dumper.DataDumper, outputFolder, serverDataFolder string) {
	dumpPackageFiles(db, schemaMetadata, data, func(row []sqlUtil.RowDataStructure, table schemareader.Table) (string, string, error) {
		storedPath := row[table.ColumnIndexes["path"]].Value
		return fmt.Sprintf("%s/%s", serverDataFolder, storedPath), fmt.Sprintf("%s/%s", outputFolder, storedPath), nil
	})
}

// DumpPackageFilesNG copies packages into the repository getPackage layout.
func DumpPackageFilesNG(db *sql.DB, schemaMetadata map[string]schemareader.Table, data dumper.DataDumper, outputFolder, serverDataFolder string) {
	dumpPackageFiles(db, schemaMetadata, data, func(row []sqlUtil.RowDataStructure, table schemareader.Table) (string, string, error) {
		return packageFilePaths(serverDataFolder, outputFolder, fmt.Sprint(row[table.ColumnIndexes["path"]].Value), row[table.ColumnIndexes["org_id"]].Value)
	})
}

func dumpPackageFiles(db *sql.DB, schemaMetadata map[string]schemareader.Table, data dumper.DataDumper,
	filePaths func([]sqlUtil.RowDataStructure, schemareader.Table) (string, string, error)) {
	packageKeysData := data.TableData["rhnpackage"]
	table := schemaMetadata[packageKeysData.TableName]

	totalPackages := len(packageKeysData.Keys)
	log.Debug().Msgf("Total package files to copy: %d", totalPackages)

	exportedpackages := 0
	processing := true

	if log.Debug().Enabled() {
		go func() {
			count := 0
			for {
				if !processing {
					break
				}
				time.Sleep(30 * time.Second)
				log.Debug().Msgf("#count: %d -- #exportedPackageFiles: #%d of %d",
					count, exportedpackages, totalPackages)
				count++
			}
		}()
	}

	exportPoint := 0
	batchSize := 500

	for len(packageKeysData.Keys) > exportPoint {
		upperLimit := exportPoint + batchSize
		if upperLimit > len(packageKeysData.Keys) {
			upperLimit = len(packageKeysData.Keys)
		}
		rows := dumper.GetRowsFromKeys(db, table, packageKeysData.Keys[exportPoint:upperLimit])
		for _, rowPackage := range rows {
			source, target, err := filePaths(rowPackage, table)
			if err != nil {
				log.Panic().Err(err).Msg("invalid package file path")
			}
			if _, err := dumper.Copy(source, target); err != nil {
				log.Panic().Err(err).Msg("could not copy package file")
			}
			exportedpackages++
		}
		exportPoint = upperLimit
	}
	processing = false
}

func packageFilePaths(serverDataFolder, destinationRoot, storedPath string, orgID interface{}) (string, string, error) {
	if storedPath == "" || strings.Contains(storedPath, `\`) {
		return "", "", fmt.Errorf("invalid package path %q", storedPath)
	}

	relativePath := filepath.FromSlash(storedPath)
	if filepath.IsAbs(relativePath) {
		return "", "", fmt.Errorf("package path must be relative: %q", storedPath)
	}
	components := strings.Split(relativePath, string(filepath.Separator))
	if len(components) < 2 {
		return "", "", fmt.Errorf("package path must contain at least two components: %q", storedPath)
	}
	for _, component := range components {
		if component == ".." || component == "." || component == "" {
			return "", "", fmt.Errorf("invalid package path component in %q", storedPath)
		}
	}

	cleanPath := filepath.Clean(relativePath)
	serverDataFolder = filepath.Clean(serverDataFolder)
	source := filepath.Join(serverDataFolder, cleanPath)
	relativeSource, err := filepath.Rel(serverDataFolder, source)
	if err != nil || relativeSource == ".." || strings.HasPrefix(relativeSource, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("package path escapes source directory: %q", storedPath)
	}

	orgDirectory := "NULL"
	if orgID != nil {
		orgDirectory = fmt.Sprintf("%s", orgID)
		if orgDirectory == "" || orgDirectory == "." || orgDirectory == ".." || strings.ContainsAny(orgDirectory, `/\\`) {
			return "", "", fmt.Errorf("invalid package organization ID %q", orgDirectory)
		}
	}
	destination := filepath.Join(destinationRoot, orgDirectory, components[len(components)-2], components[len(components)-1])
	return source, destination, nil
}
