// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultMountPoint = "/var/spacewalk"
const DefaultRepomdCacheMountPoint = "/var/cache"

// StoragePaths contains server-local package and repository metadata roots.
type StoragePaths struct {
	MountPoint            string
	RepomdCacheMountPoint string
}

func (p StoragePaths) PackageRoot() string {
	if p.MountPoint == "" {
		return DefaultMountPoint
	}
	return p.MountPoint
}

func (p StoragePaths) RepositoryMetadataRoot() string {
	if p.RepomdCacheMountPoint == "" {
		return DefaultRepomdCacheMountPoint
	}
	return p.RepomdCacheMountPoint
}

// ChannelRepositoryMetadataPath returns the server repo-md cache folder for a channel.
func (p StoragePaths) ChannelRepositoryMetadataPath(channelLabel string) string {
	return filepath.Join(p.RepositoryMetadataRoot(), "rhn", "repodata", channelLabel)
}

// ReadStoragePaths reads optional roots from the selected server configuration.
// Omitted or blank properties retain defaults; configuration I/O errors are fatal
// to the caller rather than being mistaken for absent settings.
func ReadStoragePaths(configFile string) (StoragePaths, error) {
	paths := StoragePaths{MountPoint: DefaultMountPoint, RepomdCacheMountPoint: DefaultRepomdCacheMountPoint}
	file, err := os.Open(configFile)
	if err != nil {
		return paths, fmt.Errorf("open storage configuration: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "mount_point":
			paths.MountPoint = DefaultMountPoint
			if value != "" {
				paths.MountPoint = value
			}
		case "repomd_cache_mount_point":
			paths.RepomdCacheMountPoint = DefaultRepomdCacheMountPoint
			if value != "" {
				paths.RepomdCacheMountPoint = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return paths, fmt.Errorf("read storage configuration: %w", err)
	}
	return paths, nil
}
