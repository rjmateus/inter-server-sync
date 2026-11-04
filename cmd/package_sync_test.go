// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/uyuni-project/inter-server-sync/utils"
)

func TestPackageSyncConfiguredDestination(t *testing.T) {
	for _, configText := range []string{"", "mount_point = /custom package root/\n"} {
		dir := t.TempDir()
		config := filepath.Join(dir, "rhn.conf")
		if err := os.WriteFile(config, []byte(configText), 0600); err != nil {
			t.Fatal(err)
		}
		paths, err := utils.ReadStoragePaths(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, "packages"), 0755); err != nil {
			t.Fatal(err)
		}
		called := false
		err = runPackageFileSyncWithRunner(dir, paths.PackageRoot(), func(command string, args ...string) error {
			called = true
			if len(args) > 0 && args[0] == "-v" {
				args = args[1:]
			}
			wantRoot := "/var/spacewalk"
			if configText != "" {
				wantRoot = "/custom package root"
			}
			want := []string{"-og", "--chown=wwwrun:www", "-r", filepath.Join(dir, "packages") + "/", wantRoot + "/packages/"}
			if command != "rsync" || !reflect.DeepEqual(args, want) {
				t.Errorf("command=%s args=%q, want %q", command, args, want)
			}
			return nil
		})
		if err != nil || !called {
			t.Fatalf("called=%v error=%v", called, err)
		}
	}
}

func TestPackageSyncMissingInputAndFailure(t *testing.T) {
	dir := t.TempDir()
	if err := runPackageFileSyncWithRunner(dir, utils.DefaultMountPoint, func(string, ...string) error {
		t.Fatal("must not run rsync without packages")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("rsync failed")
	if err := runPackageFileSyncWithRunner(dir, utils.DefaultMountPoint, func(string, ...string) error {
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want wrapped rsync error", err)
	}
}
