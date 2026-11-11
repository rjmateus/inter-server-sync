// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// syncChannelMetadata copies the channel metadata cache into target. deleteOption
// removes target files missing from the source; only use it when target holds
// nothing but metadata (yum repodata), never on a folder that also holds packages (deb).
func syncChannelMetadata(source, target string, deleteOption bool) error {
	return syncChannelMetadataWithRunner(source, target, deleteOption, func(command string, args ...string) ([]byte, error) {
		return exec.Command(command, args...).CombinedOutput()
	})
}

func syncChannelMetadataWithRunner(source, target string, deleteOption bool, run func(string, ...string) ([]byte, error)) error {
	source = filepath.Clean(source) + string(os.PathSeparator)
	target = filepath.Clean(target) + string(os.PathSeparator)
	args := []string{"-rt"}
	if deleteOption {
		args = append(args, "--delete")
	}
	args = append(args, source, target)
	log.Debug().Str("source", source).Str("destination", target).Str("command", "rsync").Strs("arguments", args).Msg("running channel metadata sync")

	output, err := run("rsync", args...)
	if err != nil {
		message := strings.TrimSpace(string(output))
		command := fmt.Sprintf("rsync args %q", args)
		if message != "" {
			return fmt.Errorf("%s failed for source %q and destination %q: %w: %s", command, source, target, err, message)
		}
		return fmt.Errorf("%s failed for source %q and destination %q: %w", command, source, target, err)
	}
	return nil
}
