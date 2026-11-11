// SPDX-FileCopyrightText: 2023 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/uyuni-project/inter-server-sync/cobbler"
	"github.com/uyuni-project/inter-server-sync/dumper/pillarDumper"
	"github.com/uyuni-project/inter-server-sync/schemareader"
	"github.com/uyuni-project/inter-server-sync/utils"
	"github.com/uyuni-project/inter-server-sync/xmlrpc"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import data to server",
	Run:   runImport,
}

var importDir string
var xmlRpcUser string
var xmlRpcPassword string
var xmlRpcPasswordFile string
var skipVerify bool
var certFile string
var caFile string

func init() {

	importCmd.Flags().StringVar(&importDir, "importDir", ".", "Location import data from")
	importCmd.Flags().StringVar(&xmlRpcUser, "xmlRpcUser", "admin", "A username to access the XML-RPC Api")
	importCmd.Flags().StringVar(&xmlRpcPassword, "xmlRpcPassword", "admin", "A password to access the XML-RPC Api")
	importCmd.Flags().StringVar(&xmlRpcPasswordFile, "xmlRpcPasswordFile", "", "File containing the password to access the XML-RPC Api. If set, it will override the xmlRpcPassword flag.")
	importCmd.Flags().BoolVar(&skipVerify, "skipVerify", false, "Skip verification of import signature")
	importCmd.Flags().StringVar(&certFile, "verifyKey", "hubserver.pem", "Public certificate of signign hub server")
	importCmd.Flags().StringVar(&caFile, "ca", "", "custom CA certificate chain for key validation")
	importCmd.Args = cobra.NoArgs

	rootCmd.AddCommand(importCmd)
}

func runImport(cmd *cobra.Command, args []string) {
	storagePaths, err := utils.ReadStoragePaths(serverConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Error loading storage configuration")
	}
	password, err := getXMLRPCPassword(xmlRpcPassword, xmlRpcPasswordFile)
	if err != nil {
		log.Fatal().Err(err).Msg(err.Error())
	}
	xmlRpcPassword = password

	absImportDir := utils.GetAbsPath(importDir)
	log.Info().Msg(fmt.Sprintf("starting import from dir %s", absImportDir))
	fversion, fproduct := getImportVersionProduct(absImportDir)
	sversion, sproduct := utils.GetCurrentServerVersion(serverConfig)
	if fversion != sversion || fproduct != sproduct {
		log.Panic().Msgf("Wrong version detected. Fileversion = %s ; Serverversion = %s", fversion, sversion)
	}

	// If using bundled certificate, it needs to have import dir prepended
	if certFile == "hubserver.pem" {
		certFile = path.Join(absImportDir, certFile)
	}
	// Validate we have signing key, certificate and passfile if provided
	if _, err := os.Stat(certFile); err != nil {
		log.Fatal().Err(err).Msgf("Verification public key %s does not exists. Please use `--verifyKey` to set correct public certificate.", certFile)
	}
	if len(caFile) > 0 {
		if _, err := os.Stat(caFile); err != nil {
			log.Fatal().Err(err).Msg("Provided CA file does not exists or is unreadable.")
		}
	}

	sqlImportFile := validateFolder(absImportDir)
	if !skipVerify {
		if err := utils.ValidateFile(sqlImportFile, certFile, caFile); err != nil {
			log.Fatal().Msg("Signature check of import file failed!")
		} else {
			log.Info().Msg("Import data validated")
		}
	}
	log.Info().Msg("Importing...")

	if err := runPackageFileSync(absImportDir, storagePaths.PackageRoot()); err != nil {
		log.Fatal().Err(err).Msg("error importing package files")
	}

	runImageFileSync(absImportDir, serverConfig)

	runImportSql(absImportDir, serverConfig)
	if err := syncExportedChannelRepos(absImportDir, serverConfig); err != nil {
		log.Fatal().Err(err).Msg("one or more channel repository syncs failed")
	}
	log.Info().Msg("import finished")
}

const channelArchTypeSql = `SELECT at.label
FROM rhnchannel c
JOIN rhnchannelarch ca ON ca.id = c.channel_arch_id
JOIN rhnarchtype at ON at.id = ca.arch_type_id
WHERE c.label = $1`

// debMetadataFiles mark a Debian repository in the exported repodata folder.
var debMetadataFiles = []string{"Release", "InRelease", "Packages", "Packages.gz", "Packages.xz"}

func syncExportedChannelRepos(importDirectory, serverConfig string) error {
	db := schemareader.GetDBconnection(serverConfig)
	defer db.Close()
	archType := func(label string) (string, error) {
		return lookupChannelArchType(db, label)
	}
	return syncExportedChannelReposWithRunner(importDirectory, archType, func(command string, args ...string) ([]byte, error) {
		return exec.Command(command, args...).CombinedOutput()
	})
}

// lookupChannelArchType returns the arch type label (rpm or deb) of a channel in the local database.
func lookupChannelArchType(db *sql.DB, label string) (string, error) {
	var archType string
	if err := db.QueryRow(channelArchTypeSql, label).Scan(&archType); err != nil {
		return "", err
	}
	return archType, nil
}

// resolveSyncType returns the spacewalk-repo-sync type for a channel. It uses
// the channel arch type from the database, then the metadata files, then yum.
func resolveSyncType(label, repodataPath string, archType func(string) (string, error)) string {
	dbType, err := archType(label)
	if err != nil {
		log.Warn().Err(err).Str("channel", label).Msg("could not read channel arch type from database; detecting repository type from metadata files")
	} else {
		switch strings.ToLower(strings.TrimSpace(dbType)) {
		case "deb":
			log.Debug().Str("channel", label).Str("source", "database").Msg("repository type resolved to deb")
			return "deb"
		case "rpm":
			log.Debug().Str("channel", label).Str("source", "database").Msg("repository type resolved to yum")
			return "yum"
		default:
			log.Warn().Str("channel", label).Str("archType", dbType).Msg("unknown channel arch type in database; detecting repository type from metadata files")
		}
	}

	if detected := detectRepoTypeFromFiles(repodataPath); detected != "" {
		log.Debug().Str("channel", label).Str("source", "metadata files").Msgf("repository type resolved to %s", detected)
		return detected
	}
	log.Warn().Str("channel", label).Str("repodata", repodataPath).Msg("could not determine repository type; defaulting to yum")
	return "yum"
}

// detectRepoTypeFromFiles returns yum or deb based on the metadata files, or "" if unknown.
func detectRepoTypeFromFiles(repodataPath string) string {
	if isRegularFile(filepath.Join(repodataPath, "repomd.xml")) {
		return "yum"
	}
	for _, name := range debMetadataFiles {
		if isRegularFile(filepath.Join(repodataPath, name)) {
			return "deb"
		}
	}
	return ""
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func syncExportedChannelReposWithRunner(importDirectory string, archType func(string) (string, error), run func(string, ...string) ([]byte, error)) error {
	channelsFile := filepath.Join(importDirectory, "exportedChannels.txt")
	file, err := os.Open(channelsFile)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Str("file", channelsFile).Msg("no exported channel list; skipping repository sync")
			return nil
		}
		return fmt.Errorf("open exported channel list %q: %w", channelsFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var syncErrors []string
	for scanner.Scan() {
		label := strings.TrimSpace(scanner.Text())
		if label == "" {
			continue
		}
		if label == "." || label == ".." || strings.ContainsAny(label, `/\\`) {
			log.Warn().Str("channel", label).Msg("invalid channel label in export list; skipping")
			continue
		}

		repositoryPath := filepath.Join(importDirectory, "repos", label)
		repodataPath := filepath.Join(repositoryPath, "repodata")
		// Only check that the metadata folder has content: deb channels have no
		// repomd.xml, and spacewalk-repo-sync validates the contents itself.
		entries, readErr := os.ReadDir(repodataPath)
		if readErr != nil {
			log.Warn().Err(readErr).Str("channel", label).Str("repodata", repodataPath).Msg("channel repository metadata folder is missing; skipping repo-sync")
			continue
		}
		if len(entries) == 0 {
			log.Warn().Str("channel", label).Str("repodata", repodataPath).Msg("channel repository metadata folder is empty; skipping repo-sync")
			continue
		}
		syncType := resolveSyncType(label, repodataPath, archType)

		absoluteRepositoryPath, err := filepath.Abs(repositoryPath)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("%s: resolve repository path: %v", label, err))
			continue
		}
		repositoryURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absoluteRepositoryPath)}).String()
		args := []string{"-c", label, "--type=" + syncType, "--url=" + repositoryURL}
		log.Debug().Str("channel", label).Str("command", "spacewalk-repo-sync").Strs("arguments", args).Msg("running local channel repository sync")
		output, runErr := run("spacewalk-repo-sync", args...)
		if runErr != nil {
			message := strings.TrimSpace(string(output))
			if message != "" {
				syncErrors = append(syncErrors, fmt.Sprintf("%s: %v: %s", label, runErr, message))
			} else {
				syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", label, runErr))
			}
			log.Error().Err(runErr).Str("channel", label).Str("output", message).Msg("channel repository sync failed")
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read exported channel list %q: %w", channelsFile, err)
	}
	if len(syncErrors) > 0 {
		return fmt.Errorf("channel repository sync failures: %s", strings.Join(syncErrors, "; "))
	}
	return nil
}

func getImportVersionProduct(path string) (string, string) {
	versionfile := path + "/version.txt"
	version, err := utils.ScannerFunc(versionfile, "version")
	if err != nil {
		log.Error().Msg("Version not found.")
	}
	product, err := utils.ScannerFunc(versionfile, "product_name")
	if err != nil {
		log.Error().Msg("Product not found")
	}
	log.Debug().Msgf("Import Product: %s; Version: %s", product, version)
	return version, product
}

func validateFolder(absImportDir string) string {
	out := path.Join(absImportDir, "sql_statements.sql.gz")
	_, err := os.Stat(out)
	if err != nil {
		if os.IsNotExist(err) {
			out = path.Join(absImportDir, "sql_statements.sql")
			_, err = os.Stat(out)
			if err != nil {
				log.Fatal().Err(err).Msg("No usable .sql or .gz file found in import directory")
			}
		} else {
			log.Fatal().Err(err)
		}
	}
	return out
}

func hasConfigChannels(absImportDir string) bool {
	_, err := os.Stat(fmt.Sprintf("%s/exportedConfigs.txt", absImportDir))
	log.Info().Err(err).Msg(fmt.Sprintf("no export config file found: %s/exportedConfigs.txt", absImportDir))
	return err == nil || os.IsExist(err)
}

func runPackageFileSync(absImportDir, mountPoint string) error {
	return runPackageFileSyncWithRunner(absImportDir, mountPoint, func(command string, args ...string) error {
		cmd := exec.Command(command, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	})
}

func runPackageFileSyncWithRunner(absImportDir, mountPoint string, run func(string, ...string) error) error {
	packagesImportDir := filepath.Join(absImportDir, "packages") + string(os.PathSeparator)
	err := utils.FolderExists(packagesImportDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info().Msg("no package files to import")
			return nil
		} else {
			return fmt.Errorf("get import packages folder: %w", err)
		}
	}

	rsyncParams := make([]string, 0)
	if log.Debug().Enabled() {
		rsyncParams = append(rsyncParams, "-v")
	}

	rsyncParams = append(rsyncParams, "-og", "--chown=wwwrun:www", "-r",
		packagesImportDir, filepath.Join(mountPoint, "packages")+string(os.PathSeparator))

	log.Info().Msg("starting importing package files")
	if err := run("rsync", rsyncParams...); err != nil {
		return fmt.Errorf("sync import packages: %w", err)
	}
	return nil
}

func runConfigFilesSync(labels []string, user string, password string) (interface{}, error) {
	client := xmlrpc.NewClient(user, password)
	return client.SyncConfigFiles(labels)
}

func runImageFileSync(absImportDir string, serverConfig string) {
	imagesImportDir := path.Join(absImportDir, "images")
	err := utils.FolderExists(imagesImportDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info().Msg("No image files to import")
			return
		} else {
			log.Fatal().Err(err).Msg("Error reading import folder for images")
		}
	}

	rsyncParams := make([]string, 0)
	if log.Debug().Enabled() {
		rsyncParams = append(rsyncParams, "-v")
	}
	rsyncParams = append(rsyncParams, "-og", "--chown=salt:susemanager", "--chmod=Du=rwx,Dgo=rx,Fu=rw,Fgo=r",
		"-r", "--exclude=pillars", imagesImportDir+"/", "/srv/www/os-images")

	cmd := exec.Command("rsync", rsyncParams...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Info().Msg("Copying image files")
	err = cmd.Run()
	if err != nil {
		log.Fatal().Err(err).Msg("Error importing image files")
	}
}

func importSqlFile(absImportDir string) {
	cmd := exec.Command("spacewalk-sql", fmt.Sprintf("%s/sql_statements.sql", absImportDir))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Info().Msg("Starting SQL import")
	err := cmd.Run()
	if err != nil {
		log.Fatal().Err(err).Msgf("Error running the SQL script")
	}
}

func importGzFile(absImportDir string) {
	cUnzip := exec.Command("gunzip", "-c", fmt.Sprintf("%s/sql_statements.sql.gz", absImportDir))
	cImport := exec.Command("spacewalk-sql", "-")

	pr, pw := io.Pipe()
	cUnzip.Stdout = pw
	cUnzip.Stderr = os.Stderr

	cImport.Stdin = pr
	cImport.Stdout = os.Stdout
	cImport.Stderr = os.Stderr

	log.Info().Msg("Starting SQL/GZ import")
	cUnzip.Start()
	cImport.Start()

	go func() {
		defer pw.Close()
		cUnzip.Wait()
	}()
	err := cImport.Wait()
	if err != nil {
		log.Fatal().Err(err).Msgf("Error running the SQL script")
	}
}

func runImportSql(absImportDir string, serverConfig string) {

	if _, err := os.Stat(fmt.Sprintf("%s/sql_statements.sql.gz", absImportDir)); err == nil {
		importGzFile(absImportDir)
	} else {
		if _, err := os.Stat(fmt.Sprintf("%s/sql_statements.sql", absImportDir)); err == nil {
			importSqlFile(absImportDir)
		}
	}

	pillarDumper.UpdateImagePillars(serverConfig)

	if hasConfigChannels(absImportDir) {
		labels := utils.ReadFileByLine(fmt.Sprintf("%s/exportedConfigs.txt", absImportDir))
		log.Debug().Msg("Will call xml-rpc API to update filesystem")
		_, err := runConfigFilesSync(labels, xmlRpcUser, xmlRpcPassword)
		if err != nil {
			log.Error().Err(err).Msgf(
				"Error recreating configuration files. Please run spacecmd api configchannel.syncSaltFilesOnDisk -A '[[%s]]'",
				strings.Join(labels, ", "),
			)
		}
	} else {
		log.Debug().Msg("No configuration channels, NO CALL to xml-rpc API")
	}

	log.Info().Msg("Recreating cobbler entries if needed")
	if err := cobbler.RecreateCobblerEntities(serverConfig); err != nil {
		log.Err(err).Msg("An error occured during recreating cobbler entities")
	} else {
		log.Info().Msg("Cobbler entries created")
	}
}

// getXMLRPCPassword retrieves the password. In case of multiple sources, it prioritizes:
// 1) xmlRpcPasswordFile flag
// 2) stdin
// 3) xmlRpcPassword flag
// Returns trimmed password or xmlRpcPassword
func getXMLRPCPassword(xmlRpcPassword, xmlRpcPasswordFile string) (string, error) {
	if xmlRpcPasswordFile != "" {
		pwFileContent, err := os.ReadFile(xmlRpcPasswordFile)
		if err != nil {
			return "", fmt.Errorf("failed to read password file: %w", err)
		}
		return strings.TrimSpace(string(pwFileContent)), nil
	}

	// Check if stdin is piped (not a terminal)
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat stdin: %w", err)
	}
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		reader := bufio.NewReader(os.Stdin)
		pw, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("failed to read password from stdin: %w", err)
		}
		return strings.TrimSpace(pw), nil
	}

	// fallback to xmlRpcPassword
	return strings.TrimSpace(xmlRpcPassword), nil
}
