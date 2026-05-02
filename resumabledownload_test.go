package gdrivedl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	drive "google.golang.org/api/drive/v3"
)

func TestGetDownloadBytes(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{input: "100", want: 100},
		{input: "1m", want: 10000000},
		{input: "12m", want: 12000000},
		{input: "1g", want: 1000000000},
		{input: "abc", wantErr: true},
	}
	for _, tt := range tests {
		got, err := getDownloadBytes(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("getDownloadBytes(%q) error = nil, want error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("getDownloadBytes(%q) error = %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("getDownloadBytes(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestChkResumeFile(t *testing.T) {
	t.Run("new file", func(t *testing.T) {
		v := &valResumableDownload{
			para:     para{WorkDir: t.TempDir()},
			dlParams: dlParams{DownloadFile: &drive.File{Name: "file.bin", Size: 1000}},
		}
		v.DownloadBytes = 200

		fc, end, err := v.chkResumeFile()
		if err != nil {
			t.Fatalf("chkResumeFile() error = %v", err)
		}
		if fc || end {
			t.Fatalf("chkResumeFile() = (%v, %v), want (false, false)", fc, end)
		}
		if v.Range != "bytes=0-199" {
			t.Fatalf("range = %q, want bytes=0-199", v.Range)
		}
		if v.Size != 200 {
			t.Fatalf("size = %d, want 200", v.Size)
		}
	})

	t.Run("completed file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.bin")
		if err := os.WriteFile(path, make([]byte, 1000), 0666); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		v := &valResumableDownload{
			para:     para{WorkDir: dir},
			dlParams: dlParams{DownloadFile: &drive.File{Name: "file.bin", Size: 1000}},
		}
		v.DownloadBytes = 200

		fc, end, err := v.chkResumeFile()
		if err != nil {
			t.Fatalf("chkResumeFile() error = %v", err)
		}
		if fc || !end {
			t.Fatalf("chkResumeFile() = (%v, %v), want (false, true)", fc, end)
		}
	})

	t.Run("partial file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.bin")
		if err := os.WriteFile(path, make([]byte, 300), 0666); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		v := &valResumableDownload{
			para:     para{WorkDir: dir},
			dlParams: dlParams{DownloadFile: &drive.File{Name: "file.bin", Size: 1000}},
		}
		v.DownloadBytes = 500

		fc, end, err := v.chkResumeFile()
		if err != nil {
			t.Fatalf("chkResumeFile() error = %v", err)
		}
		if !fc || end {
			t.Fatalf("chkResumeFile() = (%v, %v), want (true, false)", fc, end)
		}
		if v.Range != "bytes=300-799" {
			t.Fatalf("range = %q, want bytes=300-799", v.Range)
		}
		if v.Size != 500 {
			t.Fatalf("size = %d, want 500", v.Size)
		}
	})

	t.Run("oversized file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.bin")
		if err := os.WriteFile(path, make([]byte, 1200), 0666); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		v := &valResumableDownload{
			para:     para{WorkDir: dir, URL: "https://example.com/file"},
			dlParams: dlParams{DownloadFile: &drive.File{Name: "file.bin", Size: 1000}},
		}
		v.DownloadBytes = 500

		_, _, err := v.chkResumeFile()
		if err == nil || !strings.Contains(err.Error(), "larger than that of local file") {
			t.Fatalf("chkResumeFile() error = %v, want oversized error", err)
		}
	})
}

func TestResumableFormattingHelpers(t *testing.T) {
	indented := setIndent([][]string{{"a", "1"}, {"long", "2"}}, 0)
	if indented[0][0] != "a   " {
		t.Fatalf("setIndent() = %#v", indented)
	}
	if got := getMsg([][]string{{"a", "1"}, {"b", "2"}}, " : "); got != "a : 1\nb : 2" {
		t.Fatalf("getMsg() = %q", got)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sum.txt")
	if err := os.WriteFile(path, []byte("hello"), 0666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if got, err := getMd5Checksum(path); err != nil {
		t.Fatalf("getMd5Checksum() error = %v", err)
	} else if got != "5d41402abc4b2a76b9719d911017c592" {
		t.Fatalf("getMd5Checksum() = %q", got)
	}
}

func TestGetStatusMsg(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(path, []byte("hello"), 0666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	v := &valResumableDownload{
		para: para{WorkDir: dir, Filename: "file.bin"},
		dlParams: dlParams{
			CurrentFileSize: 5,
			DownloadFile:    &drive.File{Name: "file.bin", Size: 5, Md5Checksum: "5d41402abc4b2a76b9719d911017c592"},
			Range:           "bytes=0-4",
			Start:           0,
			End:             4,
		},
	}
	v.Size = 5

	for _, tc := range []struct {
		fc   bool
		end  bool
		want string
	}{
		{fc: false, end: false, want: "New download"},
		{fc: true, end: false, want: "Resumable download"},
		{fc: false, end: true, want: "Download has already done."},
	} {
		if got := v.getStatusMsg(tc.fc, tc.end); !strings.Contains(got, tc.want) {
			t.Fatalf("getStatusMsg(%v, %v) = %q, want substring %q", tc.fc, tc.end, got, tc.want)
		}
	}
}
