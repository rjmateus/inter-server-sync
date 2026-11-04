// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package packageDumper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uyuni-project/inter-server-sync/dumper"
	"github.com/uyuni-project/inter-server-sync/schemareader"
)

func TestPackageDumpLayouts(t *testing.T) {
	sourceRoot := t.TempDir()
	const storedPath = "packages/NULL/hash/package.rpm"
	source := filepath.Join(sourceRoot, storedPath)
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("package data"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, ng := range []bool{false, true} {
		name := "legacy"
		if ng {
			name = "ng"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			columns := []string{"id", "path", "org_id"}
			mock.ExpectQuery("SELECT id, path, org_id FROM rhnpackage").WillReturnRows(sqlmock.NewRows(columns).AddRow(1, storedPath, nil))
			schema := map[string]schemareader.Table{"rhnpackage": {
				Name: "rhnpackage", Columns: columns, ColumnIndexes: map[string]int{"id": 0, "path": 1, "org_id": 2},
			}}
			data := dumper.DataDumper{TableData: map[string]dumper.TableDump{"rhnpackage": {
				TableName: "rhnpackage", Keys: []dumper.TableKey{{Key: []dumper.RowKey{{Column: "id", Value: "1"}}}},
			}}}
			output := t.TempDir()
			target := filepath.Join(output, storedPath)
			if ng {
				output = filepath.Join(output, "repos", "channel", "getPackage")
				target = filepath.Join(output, "NULL", "hash", "package.rpm")
				DumpPackageFilesNG(db, schema, data, output, sourceRoot)
			} else {
				DumpPackageFiles(db, schema, data, output, sourceRoot)
			}
			contents, err := os.ReadFile(target)
			if err != nil || string(contents) != "package data" {
				t.Fatalf("incorrect package at %s: %q, %v", target, contents, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPackageFilePaths(t *testing.T) {
	tests := []struct {
		name       string
		storedPath string
		orgID      interface{}
		wantOrg    string
	}{
		{name: "organization ID", storedPath: "packages/NULL/e8c/release/noarch/hash/package.rpm", orgID: []byte("42"), wantOrg: "42"},
		{name: "null organization ID", storedPath: "packages/NULL/e8c/release/noarch/hash/package.rpm", orgID: nil, wantOrg: "NULL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceRoot := filepath.Join(t.TempDir(), "server-data")
			destinationRoot := filepath.Join(t.TempDir(), "repos", "channel", "getPackage")
			source, destination, err := packageFilePaths(sourceRoot, destinationRoot, tt.storedPath, tt.orgID)
			if err != nil {
				t.Fatalf("packageFilePaths() error = %v", err)
			}
			wantSource := filepath.Join(sourceRoot, filepath.FromSlash(tt.storedPath))
			wantDestination := filepath.Join(destinationRoot, tt.wantOrg, "hash", "package.rpm")
			if source != wantSource {
				t.Errorf("source = %q, want %q", source, wantSource)
			}
			if destination != wantDestination {
				t.Errorf("destination = %q, want %q", destination, wantDestination)
			}

			if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(source, []byte("package data"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := dumper.Copy(source, destination); err != nil {
				t.Fatalf("Copy() error = %v", err)
			}
			contents, err := os.ReadFile(destination)
			if err != nil {
				t.Fatalf("could not read copied package: %v", err)
			}
			if string(contents) != "package data" {
				t.Errorf("copied contents = %q, want %q", contents, "package data")
			}
		})
	}
}

func TestPackageFilePathsRejectInvalidPaths(t *testing.T) {
	invalidPaths := []string{
		"",
		"filename.rpm",
		"/absolute/path/package.rpm",
		"packages/../outside/package.rpm",
		"packages/./package.rpm",
		"packages//package.rpm",
	}

	for _, storedPath := range invalidPaths {
		t.Run(storedPath, func(t *testing.T) {
			if _, _, err := packageFilePaths("/var/spacewalk", "/export/getPackage", storedPath, nil); err == nil {
				t.Errorf("packageFilePaths(%q) error = nil, want an error", storedPath)
			}
		})
	}
}

func TestPackageFilePathsRejectInvalidOrganizationID(t *testing.T) {
	for _, orgID := range []interface{}{"../other", "a/b", "a\\b", ""} {
		if _, _, err := packageFilePaths("/var/spacewalk", "/export/getPackage", "packages/NULL/hash/package.rpm", orgID); err == nil {
			t.Errorf("packageFilePaths() with orgID %q error = nil, want an error", orgID)
		}
	}
}
