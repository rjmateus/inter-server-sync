// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func archTypeStub(archType string, err error) func(string) (string, error) {
	return func(string) (string, error) { return archType, err }
}

func writeRepodataFile(t *testing.T, importDir, label, name string) string {
	t.Helper()
	repodata := filepath.Join(importDir, "repos", label, "repodata")
	if err := os.MkdirAll(repodata, 0755); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		if err := os.WriteFile(filepath.Join(repodata, name), []byte("metadata"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return repodata
}

func writeChannelList(t *testing.T, importDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(importDir, "exportedChannels.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncExportedChannelReposWithRunner(t *testing.T) {
	importDir := t.TempDir()
	labels := []string{"channel-one", "channel-two"}
	for _, label := range labels {
		writeRepodataFile(t, importDir, label, "repomd.xml")
	}
	writeChannelList(t, importDir, "\n"+strings.Join(labels, "\n")+"\n\n")

	var calls [][]string
	err := syncExportedChannelReposWithRunner(importDir, archTypeStub("rpm", nil), func(command string, args ...string) ([]byte, error) {
		if command != "spacewalk-repo-sync" {
			t.Errorf("command = %q, want spacewalk-repo-sync", command)
		}
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	})
	if err != nil {
		t.Fatalf("syncExportedChannelReposWithRunner() error = %v", err)
	}
	if len(calls) != len(labels) {
		t.Fatalf("got %d calls, want %d", len(calls), len(labels))
	}
	absoluteImportDir, err := filepath.Abs(importDir)
	if err != nil {
		t.Fatal(err)
	}
	for index, label := range labels {
		repositoryURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(absoluteImportDir, "repos", label))}).String()
		want := []string{"-c", label, "--type=yum", "--url=" + repositoryURL}
		if !reflect.DeepEqual(calls[index], want) {
			t.Errorf("call %d args = %#v, want %#v", index, calls[index], want)
		}
	}
}

func TestSyncExportedChannelReposDebChannelWithoutRepomd(t *testing.T) {
	importDir := t.TempDir()
	writeRepodataFile(t, importDir, "deb-channel", "Release")
	writeChannelList(t, importDir, "deb-channel\n")

	var calls [][]string
	if err := syncExportedChannelReposWithRunner(importDir, archTypeStub("", sql.ErrNoRows), func(_ string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	}); err != nil {
		t.Fatalf("syncExportedChannelReposWithRunner() error = %v", err)
	}
	if len(calls) != 1 || calls[0][1] != "deb-channel" || calls[0][2] != "--type=deb" {
		t.Fatalf("calls = %#v, want one call for deb-channel with --type=deb", calls)
	}
}

func TestResolveSyncType(t *testing.T) {
	dbErr := errors.New("db down")
	for _, tt := range []struct {
		name     string
		archType string
		err      error
		file     string
		want     string
	}{
		{"database deb wins over files", "deb", nil, "repomd.xml", "deb"},
		{"database rpm", "rpm", nil, "", "yum"},
		{"database value is normalized", " DEB\n", nil, "", "deb"},
		{"database error detects yum", "", dbErr, "repomd.xml", "yum"},
		{"database error detects deb", "", dbErr, "InRelease", "deb"},
		{"no row detects deb packages", "", sql.ErrNoRows, "Packages.xz", "deb"},
		{"unknown value without markers falls back", "foo", nil, "other.txt", "yum"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repodata := writeRepodataFile(t, t.TempDir(), "channel", tt.file)
			if got := resolveSyncType("channel", repodata, archTypeStub(tt.archType, tt.err)); got != tt.want {
				t.Errorf("resolveSyncType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSyncExportedChannelReposSkipsMissingInputs(t *testing.T) {
	countingRunner := func(calls *int) func(string, ...string) ([]byte, error) {
		return func(string, ...string) ([]byte, error) {
			*calls++
			return nil, nil
		}
	}

	t.Run("missing channel list", func(t *testing.T) {
		calls := 0
		if err := syncExportedChannelReposWithRunner(t.TempDir(), archTypeStub("rpm", nil), countingRunner(&calls)); err != nil {
			t.Fatalf("syncExportedChannelReposWithRunner() error = %v", err)
		}
		if calls != 0 {
			t.Errorf("runner called %d times, want 0", calls)
		}
	})

	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, importDir string)
	}{
		{"missing repository and invalid label", func(t *testing.T, importDir string) {}},
		{"empty repodata folder", func(t *testing.T, importDir string) {
			writeRepodataFile(t, importDir, "missing-repo", "")
		}},
		{"repository without repodata folder", func(t *testing.T, importDir string) {
			if err := os.MkdirAll(filepath.Join(importDir, "repos", "missing-repo", "getPackage"), 0755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			importDir := t.TempDir()
			tt.setup(t, importDir)
			writeChannelList(t, importDir, "missing-repo\n../escape\n")
			calls := 0
			if err := syncExportedChannelReposWithRunner(importDir, archTypeStub("rpm", nil), countingRunner(&calls)); err != nil {
				t.Fatalf("syncExportedChannelReposWithRunner() error = %v", err)
			}
			if calls != 0 {
				t.Errorf("runner called %d times, want 0", calls)
			}
		})
	}
}

func TestSyncExportedChannelReposContinuesAfterFailures(t *testing.T) {
	importDir := t.TempDir()
	labels := []string{"first", "second", "third"}
	for _, label := range labels {
		writeRepodataFile(t, importDir, label, "repomd.xml")
	}
	writeChannelList(t, importDir, strings.Join(labels, "\n"))

	var called []string
	runErr := errors.New("repo-sync failed")
	err := syncExportedChannelReposWithRunner(importDir, archTypeStub("rpm", nil), func(command string, args ...string) ([]byte, error) {
		called = append(called, args[1])
		if args[1] == "first" || args[1] == "third" {
			return []byte("sync error"), runErr
		}
		return nil, nil
	})
	if !reflect.DeepEqual(called, labels) {
		t.Fatalf("called channels = %#v, want %#v", called, labels)
	}
	if err == nil {
		t.Fatal("syncExportedChannelReposWithRunner() error = nil, want aggregate error")
	}
	for _, expected := range []string{"first", "third", "sync error"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q does not contain %q", err, expected)
		}
	}
}

func TestLookupChannelArchType(t *testing.T) {
	queryErr := errors.New("query failed")
	for _, tt := range []struct {
		name    string
		expect  func(*sqlmock.ExpectedQuery)
		want    string
		wantErr error
	}{
		{"row returned", func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"label"}).AddRow("deb"))
		}, "deb", nil},
		{"no rows", func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"label"}))
		}, "", sql.ErrNoRows},
		{"query error", func(q *sqlmock.ExpectedQuery) {
			q.WillReturnError(queryErr)
		}, "", queryErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tt.expect(mock.ExpectQuery("SELECT at.label FROM rhnchannel").WithArgs("my-channel"))

			got, err := lookupChannelArchType(db, "my-channel")
			if got != tt.want || !errors.Is(err, tt.wantErr) {
				t.Errorf("lookupChannelArchType() = (%q, %v), want (%q, %v)", got, err, tt.want, tt.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
