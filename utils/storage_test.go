// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadStoragePaths(t *testing.T) {
	for _, tt := range []struct {
		name, config, mount, cache string
	}{
		{"defaults", "db_host=localhost\n", DefaultMountPoint, DefaultRepomdCacheMountPoint},
		{"empty", "", DefaultMountPoint, DefaultRepomdCacheMountPoint},
		{"custom", "mount_point=/data/packages\nrepomd_cache_mount_point=/data/cache\n", "/data/packages", "/data/cache"},
		{"mount only", "mount_point=/data\n", "/data", DefaultRepomdCacheMountPoint},
		{"cache only", "repomd_cache_mount_point=/cache\n", DefaultMountPoint, "/cache"},
		{"blank", "mount_point= \nrepomd_cache_mount_point=\t\n", DefaultMountPoint, DefaultRepomdCacheMountPoint},
		{"whitespace", "# mount_point=/ignored\n\tmount_point \t= /data with spaces/ \r\nrepomd_cache_mount_point = /cache/\n", "/data with spaces/", "/cache/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "rhn.conf")
			if err := os.WriteFile(config, []byte(tt.config), 0600); err != nil {
				t.Fatal(err)
			}
			paths, err := ReadStoragePaths(config)
			if err != nil {
				t.Fatal(err)
			}
			if paths.PackageRoot() != tt.mount || paths.RepositoryMetadataRoot() != tt.cache {
				t.Fatalf("paths = %+v, want mount %q and cache %q", paths, tt.mount, tt.cache)
			}
		})
	}
	var zero StoragePaths
	if zero.PackageRoot() != DefaultMountPoint || zero.RepositoryMetadataRoot() != DefaultRepomdCacheMountPoint {
		t.Fatal("zero-value paths must preserve defaults")
	}
}

func TestReadStoragePathsErrors(t *testing.T) {
	dir := t.TempDir()
	longFile := filepath.Join(dir, "long.conf")
	if err := os.WriteFile(longFile, []byte(strings.Repeat("x", 100000)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, config := range []string{filepath.Join(dir, "missing.conf"), dir, longFile} {
		if _, err := ReadStoragePaths(config); err == nil {
			t.Errorf("expected error for %s", config)
		}
	}
}

func TestChannelRepositoryMetadataPath(t *testing.T) {
	for _, tt := range []struct {
		name  string
		paths StoragePaths
		want  string
	}{
		{"zero value", StoragePaths{}, "/var/cache/rhn/repodata/channel-a"},
		{"default", StoragePaths{RepomdCacheMountPoint: DefaultRepomdCacheMountPoint}, "/var/cache/rhn/repodata/channel-a"},
		{"custom", StoragePaths{RepomdCacheMountPoint: "/data/cache/"}, "/data/cache/rhn/repodata/channel-a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.paths.ChannelRepositoryMetadataPath("channel-a"); got != tt.want {
				t.Errorf("ChannelRepositoryMetadataPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
