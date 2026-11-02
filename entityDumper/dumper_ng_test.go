// SPDX-FileCopyrightText: 2026 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package entityDumper

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestValidateExportFolderNg(t *testing.T) {
	for _, tt := range []struct {
		name       string
		control    string
		hasControl bool
		channels   []string
		force      bool
		approve    bool
		promptErr  error
		wantPrompt bool
		wantErr    string
	}{
		{name: "same scope", hasControl: true, control: "a\nb\n", channels: []string{"a", "b"}},
		{name: "normalized set", hasControl: true, control: " b\r\n\na\na\n", channels: []string{"b", "a", "a"}},
		{name: "addition approved", hasControl: true, control: "a\n", channels: []string{"a", "b"}, approve: true, wantPrompt: true},
		{name: "addition rejected", hasControl: true, control: "a\n", channels: []string{"a", "b"}, wantPrompt: true, wantErr: "cancelled"},
		{name: "addition forced", hasControl: true, control: "a\n", channels: []string{"a", "b"}, force: true},
		{name: "reduction", hasControl: true, control: "a\nb\n", channels: []string{"a"}, wantErr: "missing previously exported channels: b"},
		{name: "forced reduction", hasControl: true, control: "a\nb\n", channels: []string{"a"}, force: true, wantErr: "cannot reduce"},
		{name: "mixed change", hasControl: true, control: "a\nb\n", channels: []string{"a", "c"}, force: true, wantErr: "cannot reduce"},
		{name: "no channels", hasControl: true, control: "a\n", wantErr: "cannot reduce"},
		{name: "unknown scope approved", approve: true, wantPrompt: true},
		{name: "unknown scope rejected", wantPrompt: true, wantErr: "cancelled"},
		{name: "unknown scope forced", force: true},
		{name: "empty control addition", hasControl: true, channels: []string{"a"}, approve: true, wantPrompt: true},
		{name: "empty control empty selection", hasControl: true},
		{name: "prompt error", promptErr: io.EOF, wantPrompt: true, wantErr: "confirm export"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{
				"hubserver.pem": "certificate", "sql_statements.sql.gz": "old long SQL content",
				"sql_statements.sql.gz.sha512": "signature", "version.txt": "old long version",
				"repos/a/getPackage/package.rpm": "package", "unrelated.txt": "keep me",
			}
			if tt.hasControl {
				files["exportedChannels.txt"] = tt.control
			}
			for name, data := range files {
				filename := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			prompts := 0
			err := validateExportFolderNg(dir, tt.channels, tt.force, func(message string) (bool, error) {
				prompts++
				if tt.hasControl && !strings.Contains(message, "a") && !strings.Contains(message, "b") {
					t.Errorf("prompt does not name added channels: %s", message)
				}
				return tt.approve, tt.promptErr
			})
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if (prompts == 1) != tt.wantPrompt || prompts > 1 {
				t.Errorf("prompt count = %d, want prompt = %v", prompts, tt.wantPrompt)
			}
			for name, want := range files {
				data, readErr := os.ReadFile(filepath.Join(dir, name))
				preserved := tt.wantErr != "" || name == "unrelated.txt" || strings.HasPrefix(name, "repos/")
				if preserved {
					if readErr != nil || string(data) != want {
						t.Errorf("file %s changed: data=%q, error=%v", name, data, readErr)
					}
				} else if !os.IsNotExist(readErr) {
					t.Errorf("generated file %s was not removed: %v", name, readErr)
				}
			}
		})
	}
}

func TestValidateExportFolderNgEmpty(t *testing.T) {
	for _, missing := range []bool{false, true} {
		dir := t.TempDir()
		if missing {
			dir = filepath.Join(dir, "new", "export")
		}
		err := validateExportFolderNg(dir, []string{"a"}, false, func(string) (bool, error) {
			t.Fatal("empty or missing directory must not prompt")
			return false, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("export directory was not created: %v", err)
		}
	}
}

func TestValidateExportFolderNgFilesystemErrors(t *testing.T) {
	for _, target := range []string{"output", "exportedChannels.txt", "version.txt"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			if target == "output" {
				dir = filepath.Join(dir, "file")
				if err := os.WriteFile(dir, nil, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(dir, target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "hubserver.pem"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateExportFolderNg(dir, nil, true, nil); err == nil {
				t.Fatal("expected filesystem error even with force")
			}
			if target != "output" {
				data, err := os.ReadFile(filepath.Join(dir, "hubserver.pem"))
				if err != nil || string(data) != "keep" {
					t.Fatal("cleanup ran before preflight succeeded")
				}
			}
		})
	}
}

type failingPromptIO struct{}

func (failingPromptIO) Read([]byte) (int, error)  { return 0, errors.New("read failed") }
func (failingPromptIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestConfirmExportNG(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  bool
	}{
		{"y\n", true}, {" YES \r\n", true}, {"\n", false}, {"n\n", false},
		{"no\n", false}, {"maybe\n", false}, {"", false}, {"yes", false},
	} {
		var output bytes.Buffer
		got, _ := confirmExportNG(strings.NewReader(tt.input), &output, "Continue?")
		if got != tt.want || !strings.Contains(output.String(), "[y/N]") {
			t.Errorf("input %q: got %v, prompt %q", tt.input, got, output.String())
		}
	}
	if got, err := confirmExportNG(failingPromptIO{}, io.Discard, "Continue?"); got || err == nil {
		t.Fatal("input errors must abort")
	}
	if got, err := confirmExportNG(strings.NewReader("yes\n"), failingPromptIO{}, "Continue?"); got || err == nil {
		t.Fatal("output errors must abort")
	}
}

func TestValidateExportFolderNgResolvedChildren(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("select label from rhnchannel where label").WithArgs("parent").
		WillReturnRows(sqlmock.NewRows([]string{"label"}).AddRow("parent"))
	mock.ExpectQuery("select label from rhnchannel where parent_channel").WithArgs("parent").
		WillReturnRows(sqlmock.NewRows([]string{"label"}).AddRow("child"))
	channels := loadChannelsToProcess(db, nil, []string{"parent"})
	if !reflect.DeepEqual(channels, []string{"parent", "child"}) {
		t.Fatalf("unexpected effective channels: %v", channels)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "exportedChannels.txt"), []byte("parent\nchild\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateExportFolderNg(dir, channels, false, func(string) (bool, error) {
		t.Fatal("unchanged effective scope must not prompt")
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
