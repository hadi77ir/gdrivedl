package gdrivedl

import (
	"context"
	"errors"
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
