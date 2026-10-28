// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/inter-server-sync/schemareader"
)

// DumpAllEntitiesNG exports channels using repository metadata and package files.
func DumpAllEntitiesNG(options DumperOptions) {
	if err := options.loadStoragePaths(); err != nil {
		log.Fatal().Err(err).Msg("Error loading storage configuration")
	}
	outputFolderAbs := options.GetOutputFolderAbsPath()
	db := schemareader.GetDBconnection(options.ServerConfig)
	defer db.Close()
	channels := loadChannelsToProcess(db, options.ChannelLabels, options.ChannelWithChildrenLabels)
	if err := validateExportFolderNg(outputFolderAbs, channels, options.Force, func(message string) (bool, error) {
		return confirmExportNG(os.Stdin, os.Stderr, message)
	}); err != nil {
		log.Fatal().Err(err).Msg("Export directory validation failed")
	}

	outFile := path.Join(outputFolderAbs, "/sql_statements.sql.gz")
	file, err := os.OpenFile(outFile, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Panic().Err(err).Msg("error creating sql file")
	}
	defer closeAndSign(file, options.SignKey, options.PassFile)

	gzipFile := gzip.NewWriter(file)
	defer gzipFile.Close()

	bufferWriter := bufio.NewWriterSize(gzipFile, 32768)
	defer bufferWriter.Flush()

	bufferWriter.WriteString("BEGIN;\n")
	if len(options.ChannelLabels) > 0 || len(options.ChannelWithChildrenLabels) > 0 {
		processAndInsertProducts(db, bufferWriter)
		processAndInsertChannelsNG(db, bufferWriter, options, channels)
	}
	if len(options.ConfigLabels) > 0 {
		processConfigs(db, bufferWriter, options)
	}

	if options.OSImages || options.Containers {
		dumpImageData(db, bufferWriter, options)
	}

	bufferWriter.WriteString("COMMIT;\n")
}

// validateExportFolderNg checks the export scope before removing generated metadata.
// force skips confirmations only, never scope checks or filesystem errors.
func validateExportFolderNg(outputFolderAbs string, channels []string, force bool, confirm func(string) (bool, error)) error {
	if err := os.MkdirAll(outputFolderAbs, 0755); err != nil {
		return fmt.Errorf("create export directory: %w", err)
	}
	entries, err := os.ReadDir(outputFolderAbs)
	if err != nil {
		return fmt.Errorf("read export directory: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	// The legacy exporter stores package files in <output>/packages; that layout is incompatible with NG.
	legacyPackages := path.Join(outputFolderAbs, "packages")
	if info, err := os.Stat(legacyPackages); err == nil && info.IsDir() {
		return fmt.Errorf("export directory %s contains a legacy export (packages folder); it is not compatible with the NG export, use a different directory or remove it", outputFolderAbs)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect %s: %w", legacyPackages, err)
	}

	controlFile := path.Join(outputFolderAbs, "exportedChannels.txt")
	data, err := os.ReadFile(controlFile)
	var message string
	if os.IsNotExist(err) {
		message = fmt.Sprintf("No channel control file found in nonempty export directory %s. Overwrite generated metadata?", outputFolderAbs)
	} else if err != nil {
		return fmt.Errorf("read channel control file: %w", err)
	} else {
		previous := make(map[string]bool)
		for _, line := range strings.Split(string(data), "\n") {
			if label := strings.TrimSpace(line); label != "" {
				previous[label] = true
			}
		}
		requested := make(map[string]bool)
		for _, label := range channels {
			requested[label] = true
		}
		var removed, added []string
		for label := range previous {
			if !requested[label] {
				removed = append(removed, label)
			}
		}
		if len(removed) > 0 {
			sort.Strings(removed)
			return fmt.Errorf("cannot reduce export channel scope; missing previously exported channels: %s", strings.Join(removed, ", "))
		}
		for label := range requested {
			if !previous[label] {
				added = append(added, label)
			}
		}
		if len(added) > 0 {
			sort.Strings(added)
			message = fmt.Sprintf("Add channels to the existing export: %s. Continue?", strings.Join(added, ", "))
		}
	}
	if message != "" && !force {
		approved, err := confirm(message)
		if err != nil {
			return fmt.Errorf("confirm export: %w", err)
		}
		if !approved {
			return fmt.Errorf("export cancelled")
		}
	}

	generatedFiles := []string{"exportedChannels.txt", "hubserver.pem", "sql_statements.sql.gz", "sql_statements.sql.gz.sha512", "version.txt"}
	// Preflight every target so an unexpected directory cannot cause partial cleanup.
	for _, name := range generatedFiles {
		filename := path.Join(outputFolderAbs, name)
		info, err := os.Lstat(filename)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect generated file %s: %w", filename, err)
		}
		if info.IsDir() {
			return fmt.Errorf("generated file %s is a directory", filename)
		}
	}
	for _, name := range generatedFiles {
		filename := path.Join(outputFolderAbs, name)
		if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove generated file %s: %w", filename, err)
		}
	}
	return nil
}

func confirmExportNG(input io.Reader, output io.Writer, message string) (bool, error) {
	if _, err := fmt.Fprintf(output, "%s [y/N] ", message); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil {
		return false, err
	}
	answer = strings.TrimSpace(answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
