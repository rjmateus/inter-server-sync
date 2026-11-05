// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/inter-server-sync/dumper"
	"github.com/uyuni-project/inter-server-sync/dumper/packageDumper"
	"github.com/uyuni-project/inter-server-sync/schemareader"
)

// SoftwareChannelTableNamesNG excludes data supplied by repository synchronization.
func SoftwareChannelTableNamesNG() []string {
	return []string{
		"rhnchannel", "rhnchannelcloned", "suseproductchannel",
		"rhnproductname", "rhnchannelproduct", "rhnreleasechannelmap",
		"rhndistchannelmap", "rhnchannelcomps", "rhnchannelfamilymembers",
	}
}

// channelSkipExportTablesNG lists the tables whose data is not exported with the channels.
func channelSkipExportTablesNG() []string {
	return []string{"rhnerrata", "rhnchannelerrata", "rhnpackage", "rhnchannelpackage", "rhnerratapackage"}
}

func processAndInsertChannelsNG(db *sql.DB, writer *bufio.Writer, options DumperOptions, channels []string) {
	log.Info().Msgf("%d channels to process", len(channels))
	schemaMetadata := schemareader.ReadTablesSchema(db, SoftwareChannelTableNamesNG(), channelSkipExportTablesNG())
	log.Debug().Msg("channel schema metadata loaded")

	fileChannels, err := os.Create(options.GetOutputFolderAbsPath() + "/exportedChannels.txt")
	if err != nil {
		log.Panic().Err(err).Msg("error creating channel list file")
	}
	defer fileChannels.Close()
	channelWriter := bufio.NewWriter(fileChannels)
	defer channelWriter.Flush()

	for index, channelLabel := range channels {
		log.Info().Msgf("Processing channel [%d/%d] %s", index+1, len(channels), channelLabel)
		if err := os.MkdirAll(options.GetOutputFolderAbsPath()+"/repos/"+channelLabel+"/repodata", 0770); err != nil {
			log.Panic().Err(err).Msg("error creating channel repo directory")
		}
		processChannelNG(db, writer, channelLabel, schemaMetadata, options)
		writer.Flush()
		channelWriter.WriteString(channelLabel + "\n")
	}
}

func processChannelNG(db *sql.DB, writer *bufio.Writer, channelLabel string,
	schemaMetadata map[string]schemareader.Table, options DumperOptions) {
	whereFilter := fmt.Sprintf("label = '%s'", channelLabel)
	tableData := dumper.DataCrawler(db, schemaMetadata, schemaMetadata["rhnchannel"], whereFilter, options.StartingDate)

	if log.Debug().Enabled() {
		totalRows := 0
		for _, value := range tableData.TableData {
			totalRows += len(value.KeyMap)
		}
		log.Debug().Msgf("finished table data crawler. Total database rows to export: %d", totalRows)
	}

	printOptions := dumper.PrintSqlOptions{
		TablesToClean:            tablesToClean,
		CleanWhereClause:         fmt.Sprintf(`WHERE rhnchannel.id = (SELECT id FROM rhnchannel WHERE label = '%s')`, channelLabel),
		OnlyIfParentExistsTables: onlyIfParentExistsTables,
	}
	dumper.PrintTableDataOrdered(db, writer, schemaMetadata, schemaMetadata["rhnchannel"], tableData, printOptions)
	log.Debug().Msg("finished print table order")
	generateChannelChildLink(db, channelLabel, writer)

	log.Debug().Msg("dumping all package files")
	serverCacheFolder := filepath.Join(options.StoragePaths.RepositoryMetadataRoot(), "rhn/repodata", channelLabel)
	target := fmt.Sprintf("%s/repos/%s/repodata", options.GetOutputFolderAbsPath(), channelLabel)
	if err := syncChannelMetadata(serverCacheFolder, target); err != nil {
		log.Panic().Err(err).Str("source", serverCacheFolder).Str("destination", target).
			Msgf("could not sync channel metadata with rsync: %v", err)
	}
	packageFilesOutput := fmt.Sprintf("%s/repos/%s/getPackage", options.GetOutputFolderAbsPath(), channelLabel)
	packageDumper.DumpPackageFilesNG(db, schemaMetadata, tableData, packageFilesOutput, options.StoragePaths.PackageRoot())
	log.Debug().Msg("channel export finished")
}
