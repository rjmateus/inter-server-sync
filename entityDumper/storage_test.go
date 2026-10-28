// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uyuni-project/inter-server-sync/utils"
)

func TestExportStoragePathsAreIndependent(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, "custom.conf")
	defaults := filepath.Join(dir, "defaults.conf")
	if err := os.WriteFile(custom, []byte("mount_point=/custom/packages\nrepomd_cache_mount_point=/custom/cache\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaults, nil, 0600); err != nil {
		t.Fatal(err)
	}
	first := DumperOptions{ServerConfig: custom}
	second := DumperOptions{ServerConfig: defaults}
	if err := first.loadStoragePaths(); err != nil {
		t.Fatal(err)
	}
	if err := second.loadStoragePaths(); err != nil {
		t.Fatal(err)
	}
	if first.StoragePaths.PackageRoot() != "/custom/packages" || first.StoragePaths.RepositoryMetadataRoot() != "/custom/cache" {
		t.Fatalf("custom paths changed: %+v", first.StoragePaths)
	}
	if second.StoragePaths.PackageRoot() != utils.DefaultMountPoint || second.StoragePaths.RepositoryMetadataRoot() != utils.DefaultRepomdCacheMountPoint {
		t.Fatalf("custom paths leaked into defaults: %+v", second.StoragePaths)
	}
}
