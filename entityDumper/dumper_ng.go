// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"bufio"
	"compress/gzip"
	"os"
	"path"

	"github.com/rs/zerolog/log"
	"github.com/uyuni-project/inter-server-sync/schemareader"
)

// DumpAllEntitiesNG exports channels using repository metadata and package files.
func DumpAllEntitiesNG(options DumperOptions) {
	outputFolderAbs := options.GetOutputFolderAbsPath()
	validateExportFolder(outputFolderAbs)

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

	db := schemareader.GetDBconnection(options.ServerConfig)
	defer db.Close()
	bufferWriter.WriteString("BEGIN;\n")
	if len(options.ChannelLabels) > 0 || len(options.ChannelWithChildrenLabels) > 0 {
		processAndInsertProducts(db, bufferWriter)
		processAndInsertChannelsNG(db, bufferWriter, options)
	}
	if len(options.ConfigLabels) > 0 {
		processConfigs(db, bufferWriter, options)
	}

	if options.OSImages || options.Containers {
		dumpImageData(db, bufferWriter, options)
	}

	bufferWriter.WriteString("COMMIT;\n")
}
