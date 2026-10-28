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

func syncChannelMetadata(source, target string) error {
	return syncChannelMetadataWithRunner(source, target, func(command string, args ...string) ([]byte, error) {
		return exec.Command(command, args...).CombinedOutput()
	})
}

func syncChannelMetadataWithRunner(source, target string, run func(string, ...string) ([]byte, error)) error {
	source = filepath.Clean(source) + string(os.PathSeparator)
	target = filepath.Clean(target) + string(os.PathSeparator)
	args := []string{"-r", source, target}
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
