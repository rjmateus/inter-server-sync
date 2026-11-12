// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"bufio"
	"compress/gzip"
	"database/sql"
	"fmt"
	"os"
	"path"

	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/inter-server-sync/dumper"
	"github.com/uyuni-project/inter-server-sync/schemareader"
	"github.com/uyuni-project/inter-server-sync/utils"
)

// ErrataFileNG holds the errata of channels whose repository metadata carries no
// patch data (deb). It is applied on import after the repository sync, once the
// packages referenced by the errata exist on the target server.
const ErrataFileNG = "errata_statements.sql.gz"

// ErrataTableNamesNG lists the tables exported in the errata file. rhnchannel is
// the crawl root only; its row is already part of the main SQL file.
func ErrataTableNamesNG() []string {
	return []string{
		"rhnchannel",
		"rhnchannelerrata", // clean
		"rhnerrata",
		"rhnerratacloned",  // add only if there are corresponding rows in rhnerrata
		"rhnerratapackage", // clean; add only if the package exists on the target
		"rhnerratabuglist", // clean
		"rhncve",
		"rhnerratacve",     // clean
		"rhnerratakeyword", // clean
	}
}

// errataSkipExportTablesNG are loaded only to resolve foreign keys by unique key;
// their rows are created on the target by the repository sync.
func errataSkipExportTablesNG() []string {
	return []string{"rhnpackage"}
}

// errataOnlyIfParentExistsTablesNG skips errata package links whose package is
// missing on the target instead of failing the whole import transaction.
func errataOnlyIfParentExistsTablesNG() []string {
	return append(append([]string{}, onlyIfParentExistsTables...), "rhnerratapackage")
}

// debChannelsNG returns the channels whose errata are not part of the repository metadata.
func debChannelsNG(channels []string, archType func(string) (string, error)) ([]string, error) {
	result := make([]string, 0)
	for _, label := range channels {
		at, err := archType(label)
		if err != nil {
			return nil, fmt.Errorf("read arch type of channel %s: %w", label, err)
		}
		if utils.IsDebArchType(at) {
			result = append(result, label)
		}
	}
	return result, nil
}

// dumpChannelsErrataNG writes the errata of the exported deb channels to a separate
// signed SQL file. No file is created when there are no deb channels.
func dumpChannelsErrataNG(db *sql.DB, options DumperOptions, channels []string) {
	debChannels, err := debChannelsNG(channels, func(label string) (string, error) {
		return utils.LookupChannelArchType(db, label)
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Error detecting channels with errata to export")
	}
	if len(debChannels) == 0 {
		return
	}

	outFile := path.Join(options.GetOutputFolderAbsPath(), ErrataFileNG)
	file, err := os.OpenFile(outFile, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Panic().Err(err).Msg("error creating errata sql file")
	}
	defer closeAndSign(file, options.SignKey, options.PassFile)

	gzipFile := gzip.NewWriter(file)
	defer gzipFile.Close()

	bufferWriter := bufio.NewWriterSize(gzipFile, 32768)
	defer bufferWriter.Flush()

	bufferWriter.WriteString("BEGIN;\n")
	processChannelsErrataNG(db, bufferWriter, options, debChannels)
	bufferWriter.WriteString("COMMIT;\n")
}

func processChannelsErrataNG(db *sql.DB, writer *bufio.Writer, options DumperOptions, channels []string) {
	log.Info().Msgf("%d channels with errata to process", len(channels))
	schemaMetadata := schemareader.ReadTablesSchema(db, ErrataTableNamesNG(), errataSkipExportTablesNG())
	for index, channelLabel := range channels {
		log.Info().Msgf("Processing errata of channel [%d/%d] %s", index+1, len(channels), channelLabel)
		processChannelErrataNG(db, writer, channelLabel, schemaMetadata, options)
		writer.Flush()
	}
}

func processChannelErrataNG(db *sql.DB, writer *bufio.Writer, channelLabel string,
	schemaMetadata map[string]schemareader.Table, options DumperOptions) {
	whereFilter := fmt.Sprintf("label = '%s'", channelLabel)
	tableData := dumper.DataCrawler(db, schemaMetadata, schemaMetadata["rhnchannel"], whereFilter, options.StartingDate)
	// The channel row is in the main SQL file; re-applying it after the repository
	// sync would overwrite the sync state (e.g. last_synced) of the target.
	delete(tableData.TableData, "rhnchannel")

	printOptions := dumper.PrintSqlOptions{
		TablesToClean:            tablesToClean,
		CleanWhereClause:         fmt.Sprintf(`WHERE rhnchannel.id = (SELECT id FROM rhnchannel WHERE label = '%s')`, channelLabel),
		OnlyIfParentExistsTables: errataOnlyIfParentExistsTablesNG(),
		// The crawl already holds every channel link, so re-inserting the deleted rows would duplicate them.
		// With a starting date the crawl is partial and they must be restored.
		SkipRepopulate: options.StartingDate == "",
	}
	dumper.PrintTableDataOrdered(db, writer, schemaMetadata, schemaMetadata["rhnchannel"], tableData, printOptions)
	generateNeededCacheUpdateNG(channelLabel, writer)
	log.Debug().Str("channel", channelLabel).Msg("channel errata export finished")
}

// generateNeededCacheUpdateNG recalculates which systems need which patches/packages
// now that the channel errata are linked. The other steps of generateCacheCalculation
// (newest packages, repository metadata regeneration) do not depend on errata and are
// already covered by the repository sync that runs before the errata import.
func generateNeededCacheUpdateNG(channelLabel string, writer *bufio.Writer) {
	writer.WriteString(fmt.Sprintf("select rhn_channel.update_needed_cache((select id from rhnchannel where label ='%s'));\n", channelLabel))
}
