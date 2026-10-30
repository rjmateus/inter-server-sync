// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"bufio"
	"bytes"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/uyuni-project/inter-server-sync/schemareader"
)

func TestExportChannelTables(t *testing.T) {
	wantNG := []string{"rhnchannel", "rhnchannelcloned", "suseproductchannel", "rhnproductname", "rhnchannelproduct", "rhnreleasechannelmap", "rhndistchannelmap", "rhnchannelcomps", "rhnchannelfamilymembers"}
	if !reflect.DeepEqual(SoftwareChannelTableNamesNG(), wantNG) {
		t.Fatalf("unexpected NG tables: %v", SoftwareChannelTableNamesNG())
	}
	wantLegacy := append(append([]string(nil), wantNG...), strings.Fields(`
		rhnerrata rhnerratacloned rhnchannelerrata rhnpackagenevra rhnpackagename
		rhnpackagegroup rhnpackageevr rhnchecksum rhnpackage rhnchannelpackage
		rhnerratapackage rhnerratafile rhnerratafilechannel rhnerratafilepackage
		rhnerratafilepackagesource rhnpackagekeyassociation rhnpackagekey rhnerratabuglist
		rhncve rhnerratacve rhnerratakeyword rhnpackagecapability rhnpackagebreaks
		rhnpackageconflicts rhnpackageenhances rhnpackageextratag rhnpackageextratagkey
		rhnpackagefile rhnpackageobsoletes rhnpackagepredepends rhnpackageprovides
		rhnpackagerecommends rhnpackagerequires rhnsourcerpm rhnpackagesource
		rhnpackagesuggests rhnpackagesupplements susemddata susemdkeyword`)...)
	if !reflect.DeepEqual(SoftwareChannelTableNames(), wantLegacy) {
		t.Fatalf("legacy table selection changed: %v", SoftwareChannelTableNames())
	}
}

func TestChannelCacheSQLIsLegacyOnly(t *testing.T) {
	for _, tt := range []struct {
		name      string
		process   func(*sql.DB, *bufio.Writer, string, map[string]schemareader.Table, DumperOptions)
		wantCache bool
	}{
		{"legacy", processChannel, true},
		{"ng", processChannelNG, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("SELECT \\* FROM rhnchannel WHERE label = 'test-channel'").WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectQuery("select label from rhnchannel").WithArgs("test-channel").WillReturnRows(sqlmock.NewRows([]string{"label"}))
			var output bytes.Buffer
			writer := bufio.NewWriter(&output)
			schema := map[string]schemareader.Table{"rhnchannel": {Name: "rhnchannel", Export: true, Columns: []string{"id"}}}
			tt.process(db, writer, "test-channel", schema, DumperOptions{MetadataOnly: true})
			writer.Flush()
			for _, sql := range []string{"set modified = current_timestamp", "rhn_channel.update_needed_cache", "rhn_channel.refresh_newest_package", "INSERT INTO rhnRepoRegenQueue"} {
				if strings.Contains(output.String(), sql) != tt.wantCache {
					t.Errorf("cache SQL %q presence incorrect in %s export", sql, tt.name)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
