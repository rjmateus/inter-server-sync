// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package packageDumper

import (
	"os"
	"path/filepath"
	"regexp"
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
			columns := []string{"id", "path", "org_id"}
			mock.ExpectQuery("SELECT id, path, org_id FROM rhnpackage").WillReturnRows(sqlmock.NewRows(columns).AddRow(1, storedPath, nil))
			if layout == "ng-deb" {
				expectDebPackagesQuery(mock, []string{"1"}).
					WillReturnRows(debPackageRows().AddRow("1", "andromeda-dummy", nil, "2.0", "X", "all-deb"))
			}
			schema := map[string]schemareader.Table{"rhnpackage": {
				Name: "rhnpackage", Columns: columns, ColumnIndexes: map[string]int{"id": 0, "path": 1, "org_id": 2},
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
	source, destination, err := flatPackageFilePaths(sourceRoot, destinationRoot, storedPath,
		debPackage{name: "andromeda-dummy", epoch: "1", version: "2.0", release: "X", arch: "all-deb"})
	if err != nil {
		t.Fatalf("flatPackageFilePaths() error = %v", err)
	}
	if want := filepath.Join(sourceRoot, filepath.FromSlash(storedPath)); source != want {
		t.Errorf("source = %q, want %q", source, want)
	}
	if want := filepath.Join(destinationRoot, "andromeda-dummy_1:2.0-X.all-deb.deb"); destination != want {
		t.Errorf("destination = %q, want %q", destination, want)
	}
}

func TestFlatPackageFilePathsKeepsStoredNameOnIncompleteData(t *testing.T) {
	const storedPath = "packages/42/hash/andromeda-dummy-2.0-X.all-deb.deb"
	tests := []struct {
		name string
		pkg  debPackage
	}{
		{"package not found", debPackage{}},
		{"no name", debPackage{version: "2.0", release: "X", arch: "all-deb"}},
		{"no version", debPackage{name: "andromeda-dummy", arch: "all-deb"}},
		{"no arch", debPackage{name: "andromeda-dummy", version: "2.0", release: "X"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, destination, err := flatPackageFilePaths("/var/spacewalk", "/export/getPackage", storedPath, tt.pkg)
			if err != nil {
				t.Fatalf("flatPackageFilePaths() error = %v", err)
			}
			if want := filepath.Join("/export/getPackage", "andromeda-dummy-2.0-X.all-deb.deb"); destination != want {
				t.Errorf("destination = %q, want %q", destination, want)
			}
		})
	}
}

func TestFlatPackageFilePathsRejectsUnsafeGeneratedName(t *testing.T) {
	if _, _, err := flatPackageFilePaths("/var/spacewalk", "/export/getPackage", "packages/42/hash/p.deb",
		debPackage{name: "evil", version: "1/../../x", release: "1", arch: "all-deb"}); err == nil {
		t.Error("flatPackageFilePaths() error = nil, want an error for a name containing a separator")
	}
}

func TestDebPackageFileName(t *testing.T) {
	tests := []struct {
		name string
		pkg  debPackage
		want string
	}{
		{"no epoch", debPackage{name: "andromeda-dummy", version: "2.0", release: "X", arch: "all-deb"}, "andromeda-dummy_2.0-X.all-deb.deb"},
		{"epoch", debPackage{name: "andromeda-dummy", epoch: "1", version: "2.0", release: "X", arch: "all-deb"}, "andromeda-dummy_1:2.0-X.all-deb.deb"},
		{"numeric release", debPackage{name: "hello", version: "1.0", release: "1ubuntu2", arch: "amd64-deb"}, "hello_1.0-1ubuntu2.amd64-deb.deb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := debPackageFileName(tt.pkg); got != tt.want {
				t.Errorf("debPackageFileName(%+v) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}

// expectDebPackagesQuery expects the single deb package data query for ids.
func expectDebPackagesQuery(mock sqlmock.Sqlmock, ids []string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(regexp.QuoteMeta(debPackagesSql)).WithArgs(pq.Array(ids))
}

func debPackageRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "epoch", "version", "release", "label"})
}

func TestLoadDebPackages(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	table := schemareader.Table{Name: "rhnpackage", ColumnIndexes: map[string]int{"id": 0}}
	row := func(id interface{}) []sqlUtil.RowDataStructure {
		return []sqlUtil.RowDataStructure{{ColumnName: "id", Value: id}}
	}

	// Duplicate and null ids are queried once or not at all.
	expectDebPackagesQuery(mock, []string{"1", "2"}).WillReturnRows(debPackageRows().
		AddRow("1", "one", nil, "1.0", "1", "all-deb").
		AddRow("2", "two", "3", "2.0", "X", "amd64-deb"))
	packages, err := loadDebPackages(db, [][]sqlUtil.RowDataStructure{row(int64(1)), row([]byte("2")), row(int64(1)), row(nil)}, table)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]debPackage{
		"1": {name: "one", version: "1.0", release: "1", arch: "all-deb"},
		"2": {name: "two", epoch: "3", version: "2.0", release: "X", arch: "amd64-deb"},
	}
	if len(packages) != len(want) || packages["1"] != want["1"] || packages["2"] != want["2"] {
		t.Errorf("loadDebPackages() = %v, want %v", packages, want)
	}

	// A second batch gets a fresh map without the previous packages.
	expectDebPackagesQuery(mock, []string{"3"}).WillReturnRows(debPackageRows().AddRow("3", "three", "", "3.0", "1", "all-deb"))
	packages, err = loadDebPackages(db, [][]sqlUtil.RowDataStructure{row(int64(3))}, table)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages["3"].name != "three" {
		t.Errorf("second batch loadDebPackages() = %v, want only package 3", packages)
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
			if _, _, err := flatPackageFilePaths("/var/spacewalk", "/export/getPackage", storedPath,
				debPackage{name: "package", version: "1.0", release: "1", arch: "all-deb"}); err == nil {
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
			columns := []string{"id", "path", "org_id"}
			mock.ExpectQuery("SELECT id, path, org_id FROM rhnpackage").WillReturnRows(sqlmock.NewRows(columns).AddRow(1, storedPath, nil))
			if layout == "ng-deb" {
				expectDebPackagesQuery(mock, []string{"1"}).
					WillReturnRows(debPackageRows().AddRow("1", "missing", nil, "1.0", "1", "all-deb"))
			}
			schema := map[string]schemareader.Table{"rhnpackage": {
				Name: "rhnpackage", Columns: columns, ColumnIndexes: map[string]int{"id": 0, "path": 1, "org_id": 2},
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
