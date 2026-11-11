// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLookupChannelArchType(t *testing.T) {
	queryErr := errors.New("query failed")
	for _, tt := range []struct {
		name    string
		expect  func(*sqlmock.ExpectedQuery)
		want    string
		wantErr error
	}{
		{"row returned", func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"label"}).AddRow("deb"))
		}, "deb", nil},
		{"no rows", func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"label"}))
		}, "", sql.ErrNoRows},
		{"query error", func(q *sqlmock.ExpectedQuery) {
			q.WillReturnError(queryErr)
		}, "", queryErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			tt.expect(mock.ExpectQuery("SELECT at.label FROM rhnchannel").WithArgs("my-channel"))

			got, err := LookupChannelArchType(db, "my-channel")
			if got != tt.want || !errors.Is(err, tt.wantErr) {
				t.Errorf("LookupChannelArchType() = (%q, %v), want (%q, %v)", got, err, tt.want, tt.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIsDebArchType(t *testing.T) {
	for archType, want := range map[string]bool{"deb": true, " DEB\n": true, "rpm": false, "": false, "debian": false} {
		if got := IsDebArchType(archType); got != want {
			t.Errorf("IsDebArchType(%q) = %v, want %v", archType, got, want)
		}
	}
}
