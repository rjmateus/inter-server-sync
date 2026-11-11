// SPDX-FileCopyrightText: 2023 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package packageDumper

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lib/pq"
	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/inter-server-sync/dumper"
	"github.com/uyuni-project/inter-server-sync/schemareader"
	"github.com/uyuni-project/inter-server-sync/sqlUtil"
)

func DumpPackageFiles(db *sql.DB, schemaMetadata map[string]schemareader.Table, data dumper.DataDumper, outputFolder, serverDataFolder string) {
	dumpPackageFiles(db, schemaMetadata, data, nil, func(row []sqlUtil.RowDataStructure, table schemareader.Table) (string, string, error) {
		storedPath := row[table.ColumnIndexes["path"]].Value
		return fmt.Sprintf("%s/%s", serverDataFolder, storedPath), fmt.Sprintf("%s/%s", outputFolder, storedPath), nil
	})
}

// DumpPackageFilesNG copies packages into the repository getPackage layout.
// flat copies the files directly into outputFolder, as deb repositories expect,
// renamed to the "<name>_<version>..." form referenced by the Packages metadata;
// otherwise they go under <org>/<checksum>/, as yum repositories expect.
func DumpPackageFilesNG(db *sql.DB, schemaMetadata map[string]schemareader.Table, data dumper.DataDumper, outputFolder, serverDataFolder string, flat bool) {
	if flat {
		packageNames := make(map[string]string)
		dumpPackageFiles(db, schemaMetadata, data,
			func(rows [][]sqlUtil.RowDataStructure, table schemareader.Table) error {
				return loadPackageNames(db, rows, table, packageNames)
			},
			func(row []sqlUtil.RowDataStructure, table schemareader.Table) (string, string, error) {
				storedPath := fmt.Sprint(row[table.ColumnIndexes["path"]].Value)
				packageName := packageNames[columnValueString(row[table.ColumnIndexes["name_id"]].Value)]
				return flatPackageFilePaths(serverDataFolder, outputFolder, storedPath, packageName)
			})
		return
	}
	dumpPackageFiles(db, schemaMetadata, data, nil, func(row []sqlUtil.RowDataStructure, table schemareader.Table) (string, string, error) {
		storedPath := fmt.Sprint(row[table.ColumnIndexes["path"]].Value)
		return packageFilePaths(serverDataFolder, outputFolder, storedPath, row[table.ColumnIndexes["org_id"]].Value)
	})
}

const packageNamesSql = `SELECT id, name FROM rhnpackagename WHERE id = ANY($1)`

// loadPackageNames adds to packageNames (name_id -> name) the names of the batch
// rows that are not known yet, using a single query.
func loadPackageNames(db *sql.DB, rows [][]sqlUtil.RowDataStructure, table schemareader.Table, packageNames map[string]string) error {
	nameIndex, ok := table.ColumnIndexes["name_id"]
	if !ok {
		return fmt.Errorf("table %s has no name_id column", table.Name)
	}
	missing := make([]string, 0)
	seen := make(map[string]bool)
	for _, row := range rows {
		nameID := columnValueString(row[nameIndex].Value)
		if nameID == "" || seen[nameID] {
			continue
		}
		seen[nameID] = true
		if _, known := packageNames[nameID]; !known {
			missing = append(missing, nameID)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	result, err := db.Query(packageNamesSql, pq.Array(missing))
	if err != nil {
		return fmt.Errorf("query package names: %w", err)
	}
	defer result.Close()
	for result.Next() {
		var id, name string
		if err := result.Scan(&id, &name); err != nil {
			return fmt.Errorf("read package name: %w", err)
		}
		packageNames[id] = name
	}
	return result.Err()
}

// columnValueString returns the textual form of a column value; nil becomes "".
func columnValueString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

// dumpPackageFiles copies the package files of data. beforeBatch, when not nil,
// is called with each batch of rows before their file paths are resolved.
func dumpPackageFiles(db *sql.DB, schemaMetadata map[string]schemareader.Table, data dumper.DataDumper,
	beforeBatch func([][]sqlUtil.RowDataStructure, schemareader.Table) error,
	filePaths func([]sqlUtil.RowDataStructure, schemareader.Table) (string, string, error)) {
	packageKeysData := data.TableData["rhnpackage"]
	table := schemaMetadata[packageKeysData.TableName]

	totalPackages := len(packageKeysData.Keys)
	log.Debug().Msgf("Total package files to copy: %d", totalPackages)

	var exportedpackages int64
	done := make(chan struct{})
	defer close(done)

	if log.Debug().Enabled() {
		go func() {
			count := 0
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					log.Debug().Msgf("#count: %d -- #exportedPackageFiles: #%d of %d",
						count, atomic.LoadInt64(&exportedpackages), totalPackages)
					count++
				}
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
		if beforeBatch != nil {
			if err := beforeBatch(rows, table); err != nil {
				log.Panic().Err(err).Msg("could not prepare package batch")
			}
		}
		for _, rowPackage := range rows {
			source, target, err := filePaths(rowPackage, table)
			if err != nil {
				log.Panic().Err(err).Msg("invalid package file path")
			}
			if _, err := dumper.Copy(source, target); err != nil {
				log.Panic().Err(err).Msg("could not Copy File")
			}
			atomic.AddInt64(&exportedpackages, 1)
		}
		exportPoint = upperLimit
	}
}

// packageSourcePath validates the stored package path and returns the source file
// below serverDataFolder together with the path components.
func packageSourcePath(serverDataFolder, storedPath string) (string, []string, error) {
	if storedPath == "" || strings.Contains(storedPath, `\`) {
		return "", nil, fmt.Errorf("invalid package path %q", storedPath)
	}

	relativePath := filepath.FromSlash(storedPath)
	if filepath.IsAbs(relativePath) {
		return "", nil, fmt.Errorf("package path must be relative: %q", storedPath)
	}
	components := strings.Split(relativePath, string(filepath.Separator))
	if len(components) < 2 {
		return "", nil, fmt.Errorf("package path must contain at least two components: %q", storedPath)
	}
	for _, component := range components {
		if component == ".." || component == "." || component == "" {
			return "", nil, fmt.Errorf("invalid package path component in %q", storedPath)
		}
	}

	cleanPath := filepath.Clean(relativePath)
	serverDataFolder = filepath.Clean(serverDataFolder)
	source := filepath.Join(serverDataFolder, cleanPath)
	relativeSource, err := filepath.Rel(serverDataFolder, source)
	if err != nil || relativeSource == ".." || strings.HasPrefix(relativeSource, ".."+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("package path escapes source directory: %q", storedPath)
	}
	return source, components, nil
}

// flatPackageFilePaths places the package file directly in destinationRoot, using
// the deb metadata file name for packageName.
func flatPackageFilePaths(serverDataFolder, destinationRoot, storedPath, packageName string) (string, string, error) {
	source, components, err := packageSourcePath(serverDataFolder, storedPath)
	if err != nil {
		return "", "", err
	}
	fileName := components[len(components)-1]
	targetName := debPackageFileName(fileName, packageName)
	if targetName == fileName {
		log.Debug().Str("file", fileName).Str("package", packageName).Msg("deb package file name not renamed")
	}
	return source, filepath.Join(destinationRoot, targetName), nil
}

// debPackageFileName converts the stored "<name>-<version>..." file name to the
// "<name>_<version>..." form used in the deb Packages metadata. Other names are
// returned unchanged.
func debPackageFileName(fileName, packageName string) string {
	if packageName == "" || !strings.HasPrefix(fileName, packageName+"-") {
		return fileName
	}
	return packageName + "_" + strings.TrimPrefix(fileName, packageName+"-")
}

// packageFilePaths places the package file in destinationRoot/<org>/<checksum>/.
func packageFilePaths(serverDataFolder, destinationRoot, storedPath string, orgID interface{}) (string, string, error) {
	source, components, err := packageSourcePath(serverDataFolder, storedPath)
	if err != nil {
		return "", "", err
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
