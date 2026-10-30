// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"os"
	"path"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/uyuni-project/inter-server-sync/dumper"
	"github.com/uyuni-project/inter-server-sync/entityDumper"
	"github.com/uyuni-project/inter-server-sync/utils"
)

var exportNGCmd = newExportNGCommand()

func init() {
	rootCmd.AddCommand(exportNGCmd)
}

// Each command instance owns its flag values, independently of legacy export.
func newExportNGCommand() *cobra.Command {
	var options entityDumper.DumperOptionsNG
	var certificate string
	command := &cobra.Command{
		Use:   "export-ng",
		Short: "Export server entities using repository-based channel export",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runExportNG(options, certificate)
		},
	}
	flags := command.Flags()
	flags.StringSliceVar(&options.ChannelLabels, "channels", nil, "Channels to be exported")
	flags.StringSliceVar(&options.ChannelWithChildrenLabels, "channel-with-children", nil, "Channels to be exported")
	flags.StringVar(&options.OutputFolder, "outputDir", ".", "Location for generated data")
	flags.BoolVar(&options.MetadataOnly, "metadataOnly", false, "export only metadata")
	flags.BoolVar(&options.NoChangelogs, "noChangelogs", false, "Skip exporting packages changelogs")
	flags.StringVar(&options.StartingDate, "packagesOnlyAfter", "", "Only export packages added or modified after the specified date (date format can be 'YYYY-MM-DD' or 'YYYY-MM-DD hh:mm:ss')")
	flags.StringSliceVar(&options.ConfigLabels, "configChannels", nil, "Configuration Channels to be exported")
	flags.BoolVar(&options.OSImages, "images", false, "Export OS images and associated metadata")
	flags.BoolVar(&options.Containers, "containers", false, "Export containers metadata")
	flags.UintSliceVar(&options.Orgs, "orgLimit", nil, "Export only for specified organizations")
	flags.StringVar(&options.SignKey, "signKey", "/etc/pki/tls/private/spacewalk.key", "Private certificate used for signing the export")
	flags.StringVar(&certificate, "certificate", "/etc/pki/tls/certs/spacewalk.crt", "Public certificate to be included in the export. Subject of CA validation during import")
	flags.StringVar(&options.PassFile, "passfile", "", "Path to the file with certificate password if needed")
	flags.BoolVar(&options.RepoBaseExporter, "repoBaseExporter", false, "Enable exporting repository base")
	return command
}

func runExportNG(options entityDumper.DumperOptionsNG, certificate string) {
	log.Info().Msg("Export NG started")
	validatedDate, ok := utils.ValidateDate(options.StartingDate)
	if !ok {
		log.Fatal().Msg("Unable to validate the date. Allowed formats are 'YYYY-MM-DD' or 'YYYY-MM-DD hh:mm:ss'")
	}
	options.StartingDate = validatedDate
	options.ServerConfig = serverConfig

	if _, err := os.Stat(options.SignKey); err != nil {
		log.Fatal().Err(err).Msgf("Signing key %s does not exists. Please use `--signKey` to set key for export signing.", options.SignKey)
	}
	if _, err := os.Stat(certificate); errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Msgf("Public certificate %s does not exists and will not be stored. Please use `--certificate` to set certificate for export.", certificate)
	}
	if len(options.PassFile) > 0 {
		if _, err := os.Stat(options.PassFile); err != nil {
			log.Fatal().Err(err).Msg("File with private key password does not exists or is not readable.")
		}
	}

	entityDumper.DumpAllEntitiesNG(options)

	versionFile := path.Join(options.GetOutputFolderAbsPath(), "version.txt")
	vf, err := os.OpenFile(versionFile, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Panic().Err(err).Msg("Unable to create version file")
	}
	defer vf.Close()
	version, product := utils.GetCurrentServerVersion(serverConfig)
	vf.WriteString("product_name = " + product + "\n" + "version = " + version + "\n")
	if _, err := dumper.Copy(certificate, path.Join(options.GetOutputFolderAbsPath(), "hubserver.pem")); err != nil {
		log.Error().Err(err).Msg("failed to collect hub server public certificate. Manual selection will be needed on the import.")
	}
	log.Info().Msgf("Export NG done. Directory: %s", options.OutputFolder)
}
