// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDebChannelsNG(t *testing.T) {
	archTypes := map[string]string{"ubuntu": "deb", "debian": " DEB ", "sles": "rpm"}
	got, err := debChannelsNG([]string{"sles", "ubuntu", "debian"}, func(label string) (string, error) {
		return archTypes[label], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ubuntu", "debian"}; !reflect.DeepEqual(got, want) {
		t.Errorf("deb channels = %v, want %v", got, want)
	}

	_, err = debChannelsNG([]string{"broken"}, func(string) (string, error) { return "", errors.New("no row") })
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("error = %v, want error naming the channel", err)
	}
}

func TestErrataTablesNG(t *testing.T) {
	tables := ErrataTableNamesNG()
	if tables[0] != "rhnchannel" {
		t.Errorf("crawl root = %s, want rhnchannel", tables[0])
	}
	for _, skipped := range errataSkipExportTablesNG() {
		for _, table := range tables {
			if table == skipped {
				t.Errorf("table %s is both exported and skipped", table)
			}
		}
	}
	for _, required := range []string{"rhnchannelerrata", "rhnerrata", "rhnerratapackage", "rhnerratacve", "rhncve"} {
		found := false
		for _, table := range tables {
			found = found || table == required
		}
		if !found {
			t.Errorf("errata table %s is not exported", required)
		}
	}
}

func TestErrataOnlyIfParentExistsTablesNG(t *testing.T) {
	before := append([]string{}, onlyIfParentExistsTables...)
	got := errataOnlyIfParentExistsTablesNG()
	if got[len(got)-1] != "rhnerratapackage" {
		t.Errorf("only-if-parent-exists tables = %v, want rhnerratapackage included", got)
	}
	if !reflect.DeepEqual(onlyIfParentExistsTables, before) {
		t.Errorf("shared onlyIfParentExistsTables was modified: %v", onlyIfParentExistsTables)
	}
}
