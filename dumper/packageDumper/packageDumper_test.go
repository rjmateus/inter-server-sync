// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package packageDumper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/uyuni-project/inter-server-sync/dumper"
	"github.com/uyuni-project/inter-server-sync/schemareader"
	"github.com/uyuni-project/inter-server-sync/sqlUtil"
)

func TestPackageDumpLayouts(t *testing.T) {
	sourceRoot := t.TempDir()
	const storedPath = "packages/NULL/hash/andromeda-dummy-2.0-X.all-deb.deb"
	source := filepath.Join(sourceRoot, storedPath)
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("package data"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, layout := range []string{"legacy", "ng", "ng-deb"} {
		t.Run(layout, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			columns := []string{"id", "path", "org_id", "name_id"}
			mock.ExpectQuery("SELECT id, path, org_id, name_id FROM rhnpackage").WillReturnRows(sqlmock.NewRows(columns).AddRow(1, storedPath, nil, 7))
			if layout == "ng-deb" {
				mock.ExpectQuery("SELECT id, name FROM rhnpackagename WHERE id = ANY").
					WithArgs(pq.Array([]string{"7"})).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("7", "andromeda-dummy"))
			}
			schema := map[string]schemareader.Table{"rhnpackage": {
				Name: "rhnpackage", Columns: columns, ColumnIndexes: map[string]int{"id": 0, "path": 1, "org_id": 2, "name_id": 3},
			}}
			data := dumper.DataDumper{TableData: map[string]dumper.TableDump{"rhnpackage": {
				TableName: "rhnpackage", Keys: []dumper.TableKey{{Key: []dumper.RowKey{{Column: "id", Value: "1"}}}},
			}}}
			output := t.TempDir()
			target := filepath.Join(output, storedPath)
			switch layout {
			case "ng":
				output = filepath.Join(output, "repos", "channel", "getPackage")
				target = filepath.Join(output, "NULL", "hash", "andromeda-dummy-2.0-X.all-deb.deb")
				DumpPackageFilesNG(db, schema, data, output, sourceRoot, false)
			case "ng-deb":
				output = filepath.Join(output, "repos", "channel", "channel", "getPackage")
				target = filepath.Join(output, "andromeda-dummy_2.0-X.all-deb.deb")
				DumpPackageFilesNG(db, schema, data, output, sourceRoot, true)
			default:
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

func TestFlatPackageFilePaths(t *testing.T) {
	const storedPath = "packages/42/e8c/andromeda-dummy/2.0-X/all-deb/hash/andromeda-dummy-2.0-X.all-deb.deb"
	sourceRoot := filepath.Join(t.TempDir(), "server-data")
	destinationRoot := filepath.Join(t.TempDir(), "repos", "channel", "channel", "getPackage")
	source, destination, err := flatPackageFilePaths(sourceRoot, destinationRoot, storedPath, "andromeda-dummy")
	if err != nil {
		t.Fatalf("flatPackageFilePaths() error = %v", err)
	}
	if want := filepath.Join(sourceRoot, filepath.FromSlash(storedPath)); source != want {
		t.Errorf("source = %q, want %q", source, want)
	}
	if want := filepath.Join(destinationRoot, "andromeda-dummy_2.0-X.all-deb.deb"); destination != want {
		t.Errorf("destination = %q, want %q", destination, want)
	}
}

func TestDebPackageFileName(t *testing.T) {
	tests := []struct {
		name, fileName, packageName, want string
	}{
		{"rename", "andromeda-dummy-2.0-X.all-deb.deb", "andromeda-dummy", "andromeda-dummy_2.0-X.all-deb.deb"},
		{"single word name", "hello-1.0-1.amd64-deb.deb", "hello", "hello_1.0-1.amd64-deb.deb"},
		{"already metadata name", "andromeda-dummy_2.0-X.all-deb.deb", "andromeda-dummy", "andromeda-dummy_2.0-X.all-deb.deb"},
		{"prefix mismatch", "other-2.0-X.all-deb.deb", "andromeda-dummy", "other-2.0-X.all-deb.deb"},
		{"empty package name", "andromeda-dummy-2.0-X.all-deb.deb", "", "andromeda-dummy-2.0-X.all-deb.deb"},
		{"name is prefix of file name", "foo-bar-1.0-1.all-deb.deb", "foo", "foo_bar-1.0-1.all-deb.deb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := debPackageFileName(tt.fileName, tt.packageName); got != tt.want {
				t.Errorf("debPackageFileName(%q, %q) = %q, want %q", tt.fileName, tt.packageName, got, tt.want)
			}
		})
	}
}

func TestLoadPackageNamesQueriesOnlyUnknownNames(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	table := schemareader.Table{Name: "rhnpackage", ColumnIndexes: map[string]int{"name_id": 0}}
	row := func(nameID interface{}) []sqlUtil.RowDataStructure {
		return []sqlUtil.RowDataStructure{{ColumnName: "name_id", Value: nameID}}
	}
	packageNames := map[string]string{"1": "known"}
	mock.ExpectQuery("SELECT id, name FROM rhnpackagename WHERE id = ANY").
		WithArgs(pq.Array([]string{"2", "3"})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("2", "two").AddRow("3", "three"))

	rows := [][]sqlUtil.RowDataStructure{row(int64(1)), row(int64(2)), row([]byte("3")), row(int64(2)), row(nil)}
	if err := loadPackageNames(db, rows, table, packageNames); err != nil {
		t.Fatal(err)
	}
	// Every name is known now, so a second batch must not query again.
	if err := loadPackageNames(db, rows, table, packageNames); err != nil {
		t.Fatal(err)
	}
	if packageNames["2"] != "two" || packageNames["3"] != "three" || packageNames["1"] != "known" {
		t.Errorf("unexpected package names %v", packageNames)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
			if _, _, err := flatPackageFilePaths("/var/spacewalk", "/export/getPackage", storedPath, "package"); err == nil {
				t.Errorf("flatPackageFilePaths(%q) error = nil, want an error", storedPath)
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

func TestPackageDumpPanicsOnMissingFile(t *testing.T) {
	const storedPath = "packages/NULL/hash/missing.rpm"
	for _, layout := range []string{"legacy", "ng", "ng-deb"} {
		t.Run(layout, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			columns := []string{"id", "path", "org_id", "name_id"}
			mock.ExpectQuery("SELECT id, path, org_id, name_id FROM rhnpackage").WillReturnRows(sqlmock.NewRows(columns).AddRow(1, storedPath, nil, 7))
			if layout == "ng-deb" {
				mock.ExpectQuery("SELECT id, name FROM rhnpackagename WHERE id = ANY").
					WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("7", "missing"))
			}
			schema := map[string]schemareader.Table{"rhnpackage": {
				Name: "rhnpackage", Columns: columns, ColumnIndexes: map[string]int{"id": 0, "path": 1, "org_id": 2, "name_id": 3},
			}}
			data := dumper.DataDumper{TableData: map[string]dumper.TableDump{"rhnpackage": {
				TableName: "rhnpackage", Keys: []dumper.TableKey{{Key: []dumper.RowKey{{Column: "id", Value: "1"}}}},
			}}}
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic when package file cannot be copied")
				}
			}()
			switch layout {
			case "ng":
				DumpPackageFilesNG(db, schema, data, t.TempDir(), t.TempDir(), false)
			case "ng-deb":
				DumpPackageFilesNG(db, schema, data, t.TempDir(), t.TempDir(), true)
			default:
				DumpPackageFiles(db, schema, data, t.TempDir(), t.TempDir())
			}
		})
	}
}
