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

func TestSyncChannelMetadataWithRunner(t *testing.T) {
	const source = "/var/rhn/cache/test-channel"
	for _, tt := range []struct {
		name             string
		target           string
		deleteData bool
		wantArgs         []string
	}{
		{"yum repodata", "/tmp/export/repos/test-channel/repodata", true,
			[]string{"-rt", "--delete", source + "/", "/tmp/export/repos/test-channel/repodata/"}},
		{"deb repository root", "/tmp/export/repos/test-channel", false,
			[]string{"-rt", source + "/", "/tmp/export/repos/test-channel/"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			err := syncChannelMetadataWithRunner(source, tt.target, tt.deleteData, func(command string, args ...string) ([]byte, error) {
				called = true
				if command != "rsync" {
					t.Errorf("command = %q, want %q", command, "rsync")
				}
				if !reflect.DeepEqual(args, tt.wantArgs) {
					t.Errorf("args = %#v, want %#v", args, tt.wantArgs)
				}
				return nil, nil
			})
			if err != nil {
				t.Fatalf("syncChannelMetadataWithRunner() error = %v", err)
			}
			if !called {
				t.Fatal("rsync runner was not called")
			}
		})
	}
}

func TestSyncChannelMetadataWithRunnerReturnsCommandError(t *testing.T) {
	commandErr := errors.New("exit status 23")
	err := syncChannelMetadataWithRunner("/source", "/target", true, func(command string, args ...string) ([]byte, error) {
		return []byte("permission denied"), commandErr
	})
	if err == nil {
		t.Fatal("syncChannelMetadataWithRunner() error = nil, want an error")
	}
	if !errors.Is(err, commandErr) {
		t.Errorf("error %v does not wrap %v", err, commandErr)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error %q does not include rsync output", err)
	}
	for _, expected := range []string{"rsync args", "-rt", "--delete", "/source/", "/target/"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error %q does not include %q", err, expected)
		}
	}
}
