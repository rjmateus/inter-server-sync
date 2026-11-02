// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestExportCommands(t *testing.T) {
	for _, command := range []*cobra.Command{exportCmd, exportNGCmd} {
		found, _, err := rootCmd.Find([]string{command.Name()})
		if err != nil || found != command {
			t.Fatalf("command %q not registered: %v", command.Name(), err)
		}
		if err := command.Args(command, []string{"unexpected"}); err == nil {
			t.Errorf("%s accepts positional arguments", command.Name())
		}
		if err := command.Args(command, nil); err != nil {
			t.Errorf("%s rejects empty arguments: %v", command.Name(), err)
		}
	}
	if reflect.ValueOf(exportCmd.Run).Pointer() == reflect.ValueOf(exportNGCmd.Run).Pointer() {
		t.Fatal("commands share a run handler")
	}
	ng := newExportNGCommand()
	for _, legacyOnly := range []string{"noChangelogs", "packagesOnlyAfter", "metadataOnly"} {
		if ng.Flags().Lookup(legacyOnly) != nil {
			t.Errorf("export-ng unexpectedly exposes legacy-only flag %q", legacyOnly)
		}
	}
}

func TestExportFlagsAreIndependent(t *testing.T) {
	values := map[string]string{
		"channels": "channel-a,channel-b", "channel-with-children": "parent",
		"outputDir": "/tmp/export-test", "configChannels": "config-a,config-b",
		"images": "true", "containers": "true", "orgLimit": "1,2",
		"signKey": "/tmp/signing-key", "certificate": "/tmp/certificate", "passfile": "/tmp/password",
	}
	exportNGCmd.LocalNonPersistentFlags().VisitAll(func(ngFlag *pflag.Flag) {
		t.Run(ngFlag.Name, func(t *testing.T) {
			legacy := exportCmd.Flags().Lookup(ngFlag.Name)
			if legacy == nil || legacy.DefValue != ngFlag.DefValue || legacy.Value.Type() != ngFlag.Value.Type() {
				t.Fatalf("incompatible common flag %s", ngFlag.Name)
			}
			value, ok := values[ngFlag.Name]
			if !ok {
				t.Fatalf("missing test value for %s", ngFlag.Name)
			}
			original := legacy.Value.String()
			if slice, ok := legacy.Value.(pflag.SliceValue); ok {
				previous := append([]string(nil), slice.GetSlice()...)
				defer slice.Replace(previous)
			} else {
				defer legacy.Value.Set(original)
			}
			if err := ngFlag.Value.Set(value); err != nil {
				t.Fatal(err)
			}
			if legacy.Value.String() != original {
				t.Fatal("NG flag changed legacy state")
			}
			fresh := newExportNGCommand().Flags().Lookup(ngFlag.Name)
			ngValue := ngFlag.Value.String()
			if err := legacy.Value.Set(value); err != nil {
				t.Fatal(err)
			}
			if ngFlag.Value.String() != ngValue || fresh.Value.String() == ngValue {
				t.Fatal("flag state leaked between command instances")
			}
		})
	})
}
