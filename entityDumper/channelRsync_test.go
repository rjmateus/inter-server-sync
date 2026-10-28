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
	const target = "/tmp/export/repos/test-channel/repodata"

	called := false
	err := syncChannelMetadataWithRunner(source, target, func(command string, args ...string) ([]byte, error) {
		called = true
		if command != "rsync" {
			t.Errorf("command = %q, want %q", command, "rsync")
		}
		wantArgs := []string{"-rt", "--delete", source + "/", target + "/"}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Errorf("args = %#v, want %#v", args, wantArgs)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("syncChannelMetadataWithRunner() error = %v", err)
	}
	if !called {
		t.Fatal("rsync runner was not called")
	}
}

func TestSyncChannelMetadataWithRunnerReturnsCommandError(t *testing.T) {
	commandErr := errors.New("exit status 23")
	err := syncChannelMetadataWithRunner("/source", "/target", func(command string, args ...string) ([]byte, error) {
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
