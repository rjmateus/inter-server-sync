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
// named "<name>_[<epoch>:]<version>-<release>.<arch>.deb" as referenced by the
// Packages metadata;
// otherwise they go under <org>/<checksum>/, as yum repositories expect.
func DumpPackageFilesNG(db *sql.DB, schemaMetadata map[string]schemareader.Table, data dumper.DataDumper, outputFolder, serverDataFolder string, flat bool) {
	if flat {
		var packages map[string]debPackage
		dumpPackageFiles(db, schemaMetadata, data,
			func(rows [][]sqlUtil.RowDataStructure, table schemareader.Table) (err error) {
				// A new map per batch: every package is exported once, so keeping
				// previous batches would only grow memory.
				packages, err = loadDebPackages(db, rows, table)
				return err
			},
			func(row []sqlUtil.RowDataStructure, table schemareader.Table) (string, string, error) {
				storedPath := fmt.Sprint(row[table.ColumnIndexes["path"]].Value)
				pkg := packages[columnValueString(row[table.ColumnIndexes["id"]].Value)]
				return flatPackageFilePaths(serverDataFolder, outputFolder, storedPath, pkg)
			})
		return
	}
	dumpPackageFiles(db, schemaMetadata, data, nil, func(row []sqlUtil.RowDataStructure, table schemareader.Table) (string, string, error) {
		storedPath := fmt.Sprint(row[table.ColumnIndexes["path"]].Value)
		return packageFilePaths(serverDataFolder, outputFolder, storedPath, row[table.ColumnIndexes["org_id"]].Value)
	})
}

const debPackagesSql = `SELECT p.id, pn.name, pe.epoch, pe.version, pe.release, pa.label
FROM rhnpackage p
JOIN rhnpackagename pn ON pn.id = p.name_id
JOIN rhnpackageevr pe ON pe.id = p.evr_id
JOIN rhnpackagearch pa ON pa.id = p.package_arch_id
WHERE p.id = ANY($1)`

// debPackage holds the package data used to build a deb file name.
type debPackage struct {
	name, epoch, version, release, arch string
}

// loadDebPackages returns, keyed by package id, the name, EVR and arch of the
// batch rows, read with a single query.
func loadDebPackages(db *sql.DB, rows [][]sqlUtil.RowDataStructure, table schemareader.Table) (map[string]debPackage, error) {
	index, ok := table.ColumnIndexes["id"]
	if !ok {
		return nil, fmt.Errorf("table %s has no id column", table.Name)
	}
	ids := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		id := columnValueString(row[index].Value)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	packages := make(map[string]debPackage, len(ids))
	if len(ids) == 0 {
		return packages, nil
	}

	result, err := db.Query(debPackagesSql, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query deb package data: %w", err)
	}
	defer result.Close()
	for result.Next() {
		var id string
		var epoch sql.NullString
		var pkg debPackage
		if err := result.Scan(&id, &pkg.name, &epoch, &pkg.version, &pkg.release, &pkg.arch); err != nil {
			return nil, fmt.Errorf("read deb package data: %w", err)
		}
		pkg.epoch = epoch.String
		packages[id] = pkg
	}
	return packages, result.Err()
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
// the file name referenced by the deb Packages metadata. When the package data is
// incomplete the stored file name is kept.
func flatPackageFilePaths(serverDataFolder, destinationRoot, storedPath string, pkg debPackage) (string, string, error) {
	source, components, err := packageSourcePath(serverDataFolder, storedPath)
	if err != nil {
		return "", "", err
	}
	targetName := components[len(components)-1]
	if pkg.name == "" || pkg.version == "" || pkg.arch == "" {
		log.Debug().Str("file", targetName).Str("package", pkg.name).Str("version", pkg.version).Str("arch", pkg.arch).
			Msg("incomplete deb package data, keeping stored file name")
	} else {
		targetName = debPackageFileName(pkg)
	}
	if strings.ContainsAny(targetName, `/\`) || targetName == "." || targetName == ".." {
		return "", "", fmt.Errorf("invalid deb package file name %q", targetName)
	}
	return source, filepath.Join(destinationRoot, targetName), nil
}

// debPackageFileName returns the file name of a deb package as written in the
// Filename field of the Packages metadata, mimicking Uyuni's
// DebPackageWriter: <name>_[<epoch>:]<version>-<release>.<arch>.deb
func debPackageFileName(pkg debPackage) string {
	var name strings.Builder
	name.WriteString(pkg.name + "_")
	if pkg.epoch != "" {
		name.WriteString(pkg.epoch + ":")
	}
	name.WriteString(pkg.version + "-" + pkg.release + "." + pkg.arch + ".deb")
	return name.String()
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
