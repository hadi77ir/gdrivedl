package gdrivedl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeSingleDirectoryKeepsChunksByDefault(t *testing.T) {
	dir := t.TempDir()
	chunk1 := filepath.Join(dir, "chunk_0001.bin")
	chunk2 := filepath.Join(dir, "chunk_0002.bin")
	writeMergeTestFile(t, chunk1, "hello ")
	writeMergeTestFile(t, chunk2, "world")

	output := filepath.Join(dir, "joined.bin")
	if err := Merge(context.Background(), MergeRequest{Inputs: []string{dir}, Output: output}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "hello world" {
		t.Fatalf("output = %q, want %q", got, "hello world")
	}
	assertMergePathExists(t, chunk1)
	assertMergePathExists(t, chunk2)
}

func TestMergeSplitDirectoriesKeepChunksByDefault(t *testing.T) {
	root := t.TempDir()
	split1 := filepath.Join(root, "split_0001")
	split2 := filepath.Join(root, "split_0002")
	if err := os.MkdirAll(split1, 0o777); err != nil {
		t.Fatalf("MkdirAll(split1) error = %v", err)
	}
	if err := os.MkdirAll(split2, 0o777); err != nil {
		t.Fatalf("MkdirAll(split2) error = %v", err)
	}
	chunk1 := filepath.Join(split1, "0001.part")
	chunk2 := filepath.Join(split2, "0001.part")
	writeMergeTestFile(t, chunk1, "abc")
	writeMergeTestFile(t, chunk2, "def")

	output := filepath.Join(root, "joined.txt")
	if err := Merge(context.Background(), MergeRequest{Inputs: []string{root}, Output: output}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "abcdef" {
		t.Fatalf("output = %q, want %q", got, "abcdef")
	}
	assertMergePathExists(t, split1)
	assertMergePathExists(t, split2)
	assertMergePathExists(t, chunk1)
	assertMergePathExists(t, chunk2)
}

func TestMergeSafeDeleteChunksRemovesChunksAndSplitDirs(t *testing.T) {
	root := t.TempDir()
	split1 := filepath.Join(root, "split_0001")
	split2 := filepath.Join(root, "split_0002")
	if err := os.MkdirAll(split1, 0o777); err != nil {
		t.Fatalf("MkdirAll(split1) error = %v", err)
	}
	if err := os.MkdirAll(split2, 0o777); err != nil {
		t.Fatalf("MkdirAll(split2) error = %v", err)
	}
	writeMergeTestFile(t, filepath.Join(split1, "0001.part"), "abc")
	writeMergeTestFile(t, filepath.Join(split2, "0001.part"), "def")

	output := filepath.Join(root, "joined.txt")
	if err := Merge(context.Background(), MergeRequest{
		Inputs:       []string{root},
		Output:       output,
		DeleteChunks: true,
	}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "abcdef" {
		t.Fatalf("output = %q, want %q", got, "abcdef")
	}
	assertMergePathNotExists(t, split1)
	assertMergePathNotExists(t, split2)
}

func TestMergeSplitDirectoriesUseManifestAndIgnoreManifestContents(t *testing.T) {
	root := t.TempDir()
	split1 := filepath.Join(root, "split_0001")
	split2 := filepath.Join(root, "split_0002")
	if err := os.MkdirAll(split1, 0o777); err != nil {
		t.Fatalf("MkdirAll(split1) error = %v", err)
	}
	if err := os.MkdirAll(split2, 0o777); err != nil {
		t.Fatalf("MkdirAll(split2) error = %v", err)
	}
	writeMergeTestFile(t, filepath.Join(split1, "model.chunk00000001-of-00000004"), "aa")
	writeMergeTestFile(t, filepath.Join(split1, "model.chunk00000002-of-00000004"), "bb")
	writeMergeTestFile(t, filepath.Join(split2, "model.chunk00000003-of-00000004"), "cc")
	writeMergeTestFile(t, filepath.Join(split2, "model.chunk00000004-of-00000004"), "dd")
	writeMergeTestFile(t, filepath.Join(split1, "notes.txt"), "ignore me")
	writeMergeManifestFile(t, filepath.Join(split1, "model.split_0001.manifest.json"), "model.chunk########-of-00000004", 1, 2)
	writeMergeManifestFile(t, filepath.Join(split2, "model.split_0002.manifest.json"), "model.chunk########-of-00000004", 3, 4)

	output := filepath.Join(root, "joined.bin")
	if err := Merge(context.Background(), MergeRequest{Inputs: []string{root}, Output: output}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "aabbccdd" {
		t.Fatalf("output = %q, want %q", got, "aabbccdd")
	}
	assertMergePathExists(t, filepath.Join(split1, "model.split_0001.manifest.json"))
	assertMergePathExists(t, filepath.Join(split2, "model.split_0002.manifest.json"))
	assertMergePathExists(t, filepath.Join(split1, "notes.txt"))
}

func TestMergeManifestBackedSplitDirectoriesDeleteChunksRemovesDirs(t *testing.T) {
	root := t.TempDir()
	split1 := filepath.Join(root, "split_0001")
	split2 := filepath.Join(root, "split_0002")
	if err := os.MkdirAll(split1, 0o777); err != nil {
		t.Fatalf("MkdirAll(split1) error = %v", err)
	}
	if err := os.MkdirAll(split2, 0o777); err != nil {
		t.Fatalf("MkdirAll(split2) error = %v", err)
	}
	writeMergeTestFile(t, filepath.Join(split1, "model.chunk00000001-of-00000002"), "aa")
	writeMergeTestFile(t, filepath.Join(split2, "model.chunk00000002-of-00000002"), "bb")
	writeMergeManifestFile(t, filepath.Join(split1, "model.split_0001.manifest.json"), "model.chunk########-of-00000002", 1, 1)
	writeMergeManifestFile(t, filepath.Join(split2, "model.split_0002.manifest.json"), "model.chunk########-of-00000002", 2, 2)

	output := filepath.Join(root, "joined.bin")
	if err := Merge(context.Background(), MergeRequest{Inputs: []string{root}, Output: output, DeleteChunks: true}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "aabb" {
		t.Fatalf("output = %q, want %q", got, "aabb")
	}
	assertMergePathNotExists(t, split1)
	assertMergePathNotExists(t, split2)
}

func TestMergeSupportsDirectSplitDirectoryInput(t *testing.T) {
	root := t.TempDir()
	splitDir := filepath.Join(root, "split_0001")
	if err := os.MkdirAll(splitDir, 0o777); err != nil {
		t.Fatalf("MkdirAll(splitDir) error = %v", err)
	}
	writeMergeTestFile(t, filepath.Join(splitDir, "archive.part001"), "abc")
	writeMergeTestFile(t, filepath.Join(splitDir, "archive.part002"), "def")

	output := filepath.Join(root, "joined.bin")
	if err := Merge(context.Background(), MergeRequest{Inputs: []string{splitDir}, Output: output}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "abcdef" {
		t.Fatalf("output = %q, want %q", got, "abcdef")
	}
}

func TestMergeSupportsCommonChunkNamePatterns(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  string
	}{
		{
			name:  "numeric extension",
			files: []string{"archive.001", "archive.002"},
			want:  "abcdef",
		},
		{
			name:  "part suffix",
			files: []string{"archive.part001", "archive.part002"},
			want:  "abcdef",
		},
		{
			name:  "chunk suffix",
			files: []string{"archive.chunk001", "archive.chunk002"},
			want:  "abcdef",
		},
		{
			name:  "chunk of total",
			files: []string{"archive.chunk00000001-of-00000002", "archive.chunk00000002-of-00000002"},
			want:  "abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeMergeTestFile(t, filepath.Join(dir, tt.files[0]), "abc")
			writeMergeTestFile(t, filepath.Join(dir, tt.files[1]), "def")
			writeMergeTestFile(t, filepath.Join(dir, "ignored.txt"), "ignore me")

			output := filepath.Join(dir, "joined.bin")
			if err := Merge(context.Background(), MergeRequest{Inputs: []string{dir}, Output: output}); err != nil {
				t.Fatalf("Merge() error = %v", err)
			}

			if got := readMergeTestFile(t, output); got != tt.want {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMergeDryRunListsChunksInAlphanumericAscendingOrder(t *testing.T) {
	dir := t.TempDir()
	chunk1 := filepath.Join(dir, "archive.chunk001")
	chunk2 := filepath.Join(dir, "archive.chunk002")
	chunk10 := filepath.Join(dir, "archive.chunk010")
	writeMergeTestFile(t, chunk10, "ten")
	writeMergeTestFile(t, chunk2, "two")
	writeMergeTestFile(t, chunk1, "one")
	writeMergeTestFile(t, filepath.Join(dir, "ignored.txt"), "ignore")

	output := filepath.Join(dir, "joined.bin")
	stdout := captureStdout(t, func() {
		if err := Merge(context.Background(), MergeRequest{Inputs: []string{dir}, Output: output, DryRun: true}); err != nil {
			t.Fatalf("Merge() error = %v", err)
		}
	})
	lines := splitNonEmptyLines(stdout)
	want := []string{chunk1, chunk2, chunk10}
	if len(lines) != len(want) {
		t.Fatalf("dry-run lines = %#v, want %#v", lines, want)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("dry-run line %d = %q, want %q", index, lines[index], want[index])
		}
	}
	assertMergePathNotExists(t, output)
	assertMergePathExists(t, chunk1)
	assertMergePathExists(t, chunk2)
	assertMergePathExists(t, chunk10)
}

func TestMergeDryRunUsesManifestOrderAndDoesNotMergeManifestFile(t *testing.T) {
	root := t.TempDir()
	splitDir := filepath.Join(root, "split_0001")
	if err := os.MkdirAll(splitDir, 0o777); err != nil {
		t.Fatalf("MkdirAll(splitDir) error = %v", err)
	}
	chunk1 := filepath.Join(splitDir, "model.chunk00000001-of-00000002")
	chunk2 := filepath.Join(splitDir, "model.chunk00000002-of-00000002")
	manifest := filepath.Join(splitDir, "model.split_0001.manifest.json")
	writeMergeTestFile(t, chunk2, "bb")
	writeMergeTestFile(t, chunk1, "aa")
	writeMergeManifestFile(t, manifest, "model.chunk########-of-00000002", 1, 2)

	output := filepath.Join(root, "joined.bin")
	stdout := captureStdout(t, func() {
		if err := Merge(context.Background(), MergeRequest{Inputs: []string{splitDir}, Output: output, DryRun: true}); err != nil {
			t.Fatalf("Merge() error = %v", err)
		}
	})
	lines := splitNonEmptyLines(stdout)
	want := []string{chunk1, chunk2}
	if len(lines) != len(want) {
		t.Fatalf("dry-run lines = %#v, want %#v", lines, want)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("dry-run line %d = %q, want %q", index, lines[index], want[index])
		}
	}
	assertMergePathNotExists(t, output)
	assertMergePathExists(t, manifest)
}

func TestMergeUnsafeDeletesChunks(t *testing.T) {
	dir := t.TempDir()
	chunk1 := filepath.Join(dir, "chunk_0001.bin")
	chunk2 := filepath.Join(dir, "chunk_0002.bin")
	writeMergeTestFile(t, chunk1, "hello ")
	writeMergeTestFile(t, chunk2, "world")

	output := filepath.Join(dir, "joined.bin")
	if err := Merge(context.Background(), MergeRequest{
		Inputs: []string{dir},
		Output: output,
		Unsafe: true,
	}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "hello world" {
		t.Fatalf("output = %q, want %q", got, "hello world")
	}
	assertMergePathNotExists(t, chunk1)
	assertMergePathNotExists(t, chunk2)
}

func TestMergeSafeModeCancellationKeepsChunksAndRemovesPartialOutput(t *testing.T) {
	dir := t.TempDir()
	chunk := filepath.Join(dir, "chunk_0001.bin")
	writeMergeTestFile(t, chunk, "hello")
	output := filepath.Join(dir, "joined.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Merge(ctx, MergeRequest{Inputs: []string{dir}, Output: output})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Merge() error = %v, want context.Canceled", err)
	}

	assertMergePathExists(t, chunk)
	assertMergePathNotExists(t, output)
	partials, globErr := filepath.Glob(output + ".gdrivedl-merge-partial*")
	if globErr != nil {
		t.Fatalf("Glob() error = %v", globErr)
	}
	if len(partials) != 0 {
		t.Fatalf("partial outputs should be cleaned up, got %v", partials)
	}
}

func TestMergeUnsafeModeIgnoresCancellation(t *testing.T) {
	dir := t.TempDir()
	chunk := filepath.Join(dir, "chunk_0001.bin")
	writeMergeTestFile(t, chunk, "hello")
	output := filepath.Join(dir, "joined.bin")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Merge(ctx, MergeRequest{Inputs: []string{dir}, Output: output, Unsafe: true}); err != nil {
		t.Fatalf("Merge() error = %v", err)
	}

	if got := readMergeTestFile(t, output); got != "hello" {
		t.Fatalf("output = %q, want %q", got, "hello")
	}
	assertMergePathNotExists(t, chunk)
}

func TestMergeRejectsDeleteChunksUnsafe(t *testing.T) {
	dir := t.TempDir()
	chunk := filepath.Join(dir, "chunk_0001.bin")
	writeMergeTestFile(t, chunk, "hello")
	output := filepath.Join(dir, "joined.bin")

	err := Merge(context.Background(), MergeRequest{
		Inputs:       []string{dir},
		Output:       output,
		DeleteChunks: true,
		Unsafe:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "DeleteChunks cannot be combined with Unsafe") {
		t.Fatalf("Merge() error = %v, want invalid combination error", err)
	}
	assertMergePathExists(t, chunk)
	assertMergePathNotExists(t, output)
}

func TestMergeRejectsExistingOutputWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	chunk := filepath.Join(dir, "chunk_0001.bin")
	output := filepath.Join(dir, "joined.bin")
	writeMergeTestFile(t, chunk, "hello")
	writeMergeTestFile(t, output, "existing")

	err := Merge(context.Background(), MergeRequest{Inputs: []string{dir}, Output: output})
	if err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("Merge() error = %v, want file exists", err)
	}
	assertMergePathExists(t, chunk)
}

func TestCreateHelpIncludesGetAndMergeCommands(t *testing.T) {
	app := createHelp()
	commands := map[string]bool{}
	for _, command := range app.Commands {
		commands[command.Name] = true
	}
	if !commands["get"] {
		t.Fatal("get command should be registered")
	}
	if !commands["merge"] {
		t.Fatal("merge command should be registered")
	}
}

func writeMergeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o666); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func writeMergeManifestFile(t *testing.T, path, pattern string, first, last int) {
	t.Helper()
	contents := strings.Join([]string{
		"{",
		fmt.Sprintf("  \"chunk_filename_pattern\": %q,", pattern),
		fmt.Sprintf("  \"first_chunk_in_this_split_part\": %d,", first),
		fmt.Sprintf("  \"last_chunk_in_this_split_part\": %d", last),
		"}",
	}, "\n") + "\n"
	writeMergeTestFile(t, path, contents)
}

func readMergeTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func assertMergePathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s should exist, stat err = %v", path, err)
	}
}

func assertMergePathNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s should not exist, stat err = %v", path, err)
	}
}

func splitNonEmptyLines(value string) []string {
	parts := strings.Split(value, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}
