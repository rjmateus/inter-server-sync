// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSyncExportedChannelReposWithRunner(t *testing.T) {
	importDir := t.TempDir()
	labels := []string{"channel-one", "channel-two"}
	for _, label := range labels {
		repomd := filepath.Join(importDir, "repos", label, "repodata", "repomd.xml")
		if err := os.MkdirAll(filepath.Dir(repomd), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(repomd, []byte("metadata"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	channelList := "\n" + strings.Join(labels, "\n") + "\n\n"
	if err := os.WriteFile(filepath.Join(importDir, "exportedChannels.txt"), []byte(channelList), 0644); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	err := syncExportedChannelReposWithRunner(importDir, func(command string, args ...string) ([]byte, error) {
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
		want := []string{"-c", label, "--type", "yum", "--url=" + repositoryURL}
		if !reflect.DeepEqual(calls[index], want) {
			t.Errorf("call %d args = %#v, want %#v", index, calls[index], want)
		}
	}
}

func TestSyncExportedChannelReposSkipsMissingInputs(t *testing.T) {
	t.Run("missing channel list", func(t *testing.T) {
		calls := 0
		if err := syncExportedChannelReposWithRunner(t.TempDir(), func(string, ...string) ([]byte, error) {
			calls++
			return nil, nil
		}); err != nil {
			t.Fatalf("syncExportedChannelReposWithRunner() error = %v", err)
		}
		if calls != 0 {
			t.Errorf("runner called %d times, want 0", calls)
		}
	})

	t.Run("incomplete repository and invalid label", func(t *testing.T) {
		importDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(importDir, "exportedChannels.txt"), []byte("missing-repomd\n../escape\n"), 0644); err != nil {
			t.Fatal(err)
		}
		calls := 0
		if err := syncExportedChannelReposWithRunner(importDir, func(string, ...string) ([]byte, error) {
			calls++
			return nil, nil
		}); err != nil {
			t.Fatalf("syncExportedChannelReposWithRunner() error = %v", err)
		}
		if calls != 0 {
			t.Errorf("runner called %d times, want 0", calls)
		}
	})
}

func TestSyncExportedChannelReposContinuesAfterFailures(t *testing.T) {
	importDir := t.TempDir()
	labels := []string{"first", "second", "third"}
	for _, label := range labels {
		repomd := filepath.Join(importDir, "repos", label, "repodata", "repomd.xml")
		if err := os.MkdirAll(filepath.Dir(repomd), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(repomd, []byte("metadata"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(importDir, "exportedChannels.txt"), []byte(strings.Join(labels, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	var called []string
	runErr := errors.New("repo-sync failed")
	err := syncExportedChannelReposWithRunner(importDir, func(command string, args ...string) ([]byte, error) {
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
