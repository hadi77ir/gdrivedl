package gdrivedl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type MergeRequest struct {
	DeleteChunks         bool
	DryRun               bool
	Inputs               []string
	Output               string
	Overwrite            bool
	EventObserver        EventObserver
	ExitReport           bool
	JSONOutput           bool
	ShowTerminalProgress bool
	Unsafe               bool
	Verbosity            int
}

type mergePlan struct {
	chunks       []mergeChunk
	cleanupDirs  []string
	cleanupFiles []string
	totalSize    int64
	outputPath   string
}

type mergeManifest struct {
	ChunkFilenamePattern      string `json:"chunk_filename_pattern"`
	FirstChunkInThisSplitPart int    `json:"first_chunk_in_this_split_part"`
	LastChunkInThisSplitPart  int    `json:"last_chunk_in_this_split_part"`
}

type mergeChunk struct {
	path string
	size int64
}

type mergeOutputTarget struct {
	file     *os.File
	path     string
	tempPath string
	safeMode bool
}

func Merge(ctx context.Context, req MergeRequest) error {
	return MergeWithObserver(ctx, req, nil)
}

func MergeWithObserver(ctx context.Context, req MergeRequest, observer ProgressObserver) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.Verbosity < 0 {
		return fmt.Errorf("Verbosity must be greater than or equal to 0")
	}
	if req.DeleteChunks && req.Unsafe {
		return fmt.Errorf("DeleteChunks cannot be combined with Unsafe")
	}
	plan, err := newMergePlan(req.Inputs, req.Output)
	if err != nil {
		return err
	}
	runtime := (*downloadRuntime)(nil)
	showProgress := req.ShowTerminalProgress && !req.DryRun
	if observer != nil || showProgress || req.ExitReport || req.EventObserver != nil || req.JSONOutput {
		runtime = newObservedDownloadRuntime(showProgress, req.ExitReport, req.JSONOutput, observer, req.EventObserver)
		runtime.start()
		defer runtime.finish()
	}
	task := (*downloadTask)(nil)
	if runtime != nil {
		task = runtime.newTask(filepath.Base(plan.outputPath), plan.outputPath)
		task.SetName(filepath.Base(plan.outputPath))
		task.SetTotal(plan.totalSize)
		defer func() {
			if err != nil {
				finishTaskWithError(task, err)
			}
		}()
	}
	mergeLog(runtime, req.Verbosity, 1, "merge_plan", "[merge] output=%s chunks=%d total=%d mode=%s delete_chunks=%t\n", plan.outputPath, len(plan.chunks), plan.totalSize, mergeModeName(req), req.DeleteChunks)
	mergeReport(runtime, "merge_plan", map[string]any{
		"mode":          mergeModeName(req),
		"delete_chunks": req.DeleteChunks,
		"dry_run":       req.DryRun,
		"output":        plan.outputPath,
		"chunk_count":   len(plan.chunks),
		"total_size":    plan.totalSize,
	})
	if req.DryRun {
		return completeMergeDryRun(plan, runtime, task, req)
	}
	outputTarget, err := openMergeOutputTarget(plan.outputPath, req.Overwrite, !req.Unsafe)
	if err != nil {
		return err
	}
	defer func() {
		if outputTarget.file != nil {
			_ = outputTarget.file.Close()
		}
		if err != nil && outputTarget.safeMode && outputTarget.tempPath != "" {
			_ = os.Remove(outputTarget.tempPath)
		}
	}()
	if task != nil {
		task.MarkStarted()
	}
	for _, chunk := range plan.chunks {
		if !req.Unsafe {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if outputTarget.file == nil {
			return fmt.Errorf("merge output is not open")
		}
		if task != nil {
			task.SetState("writing chunk")
			task.SetDetail(filepath.Base(chunk.path))
		}
		mergeLog(runtime, req.Verbosity, 1, "merge_chunk_start", "[merge] writing chunk=%s size=%d mode=%s\n", chunk.path, chunk.size, mergeModeName(req))
		sourceFile, err := os.Open(chunk.path)
		if err != nil {
			return err
		}
		sourceReader := io.ReadCloser(sourceFile)
		if !req.Unsafe {
			sourceReader = &mergeContextReadCloser{ctx: ctx, ReadCloser: sourceReader}
		}
		reader := io.Reader(sourceReader)
		if task != nil {
			reader = &trackedReadCloser{ReadCloser: sourceReader, task: task}
		}
		_, copyErr := io.Copy(outputTarget.file, reader)
		closeErr := sourceReader.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if req.Unsafe {
			if task != nil {
				task.SetState("deleting chunk")
			}
			if err := os.Remove(chunk.path); err != nil {
				return err
			}
			mergeLog(runtime, req.Verbosity, 2, "merge_chunk_deleted", "[merge] deleted chunk=%s\n", chunk.path)
		}
		mergeReport(runtime, "merge_chunk", map[string]any{
			"mode":  mergeModeName(req),
			"chunk": chunk.path,
			"size":  chunk.size,
		})
	}
	if task != nil {
		task.SetState("finalizing output")
	}
	if err := outputTarget.file.Sync(); err != nil {
		return err
	}
	if err := finalizeMergeOutput(&outputTarget, req.Overwrite, task); err != nil {
		return err
	}
	if req.Unsafe {
		if task != nil {
			task.SetState("cleaning up")
		}
		if err := cleanupMergePaths(plan.cleanupFiles, runtime, req.Verbosity); err != nil {
			return err
		}
		cleanupMergeDirs(plan.cleanupDirs)
	} else if req.DeleteChunks {
		if task != nil {
			task.SetState("cleaning up")
		}
		if err := cleanupMergeChunks(plan.chunks, runtime, req.Verbosity); err != nil {
			return err
		}
		if err := cleanupMergePaths(plan.cleanupFiles, runtime, req.Verbosity); err != nil {
			return err
		}
		cleanupMergeDirs(plan.cleanupDirs)
	}
	if task != nil {
		task.SetState("merge complete")
		task.SetDetail("")
		task.MarkCompleted()
	}
	mergeReport(runtime, "merge_complete", map[string]any{
		"mode":          mergeModeName(req),
		"delete_chunks": req.DeleteChunks,
		"output":        plan.outputPath,
		"chunk_count":   len(plan.chunks),
		"total_size":    plan.totalSize,
	})
	mergeLog(runtime, req.Verbosity, 1, "merge_complete", "[merge] completed output=%s mode=%s\n", plan.outputPath, mergeModeName(req))
	return nil
}

func newMergePlan(inputs []string, output string) (mergePlan, error) {
	if strings.TrimSpace(output) == "" {
		return mergePlan{}, fmt.Errorf("an output file is required")
	}
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return mergePlan{}, err
	}
	if len(inputs) == 0 {
		inputs = []string{"."}
	}
	seenChunks := map[string]struct{}{}
	seenCleanupDirs := map[string]struct{}{}
	seenCleanupFiles := map[string]struct{}{}
	plan := mergePlan{outputPath: outputPath}
	for _, input := range inputs {
		chunks, cleanupDirs, cleanupFiles, err := discoverMergeInput(input, outputPath)
		if err != nil {
			return mergePlan{}, err
		}
		for _, chunk := range chunks {
			if _, ok := seenChunks[chunk.path]; ok {
				continue
			}
			seenChunks[chunk.path] = struct{}{}
			plan.totalSize += chunk.size
			plan.chunks = append(plan.chunks, chunk)
		}
		for _, dir := range cleanupDirs {
			if _, ok := seenCleanupDirs[dir]; ok {
				continue
			}
			seenCleanupDirs[dir] = struct{}{}
			plan.cleanupDirs = append(plan.cleanupDirs, dir)
		}
		for _, file := range cleanupFiles {
			if _, ok := seenCleanupFiles[file]; ok {
				continue
			}
			seenCleanupFiles[file] = struct{}{}
			plan.cleanupFiles = append(plan.cleanupFiles, file)
		}
	}
	if len(plan.chunks) == 0 {
		return mergePlan{}, fmt.Errorf("no chunk files were found")
	}
	for _, chunk := range plan.chunks {
		if sameFilePath(chunk.path, plan.outputPath) {
			return mergePlan{}, fmt.Errorf("output file cannot be one of the source chunks")
		}
	}
	return plan, nil
}

func discoverMergeInput(input, outputPath string) ([]mergeChunk, []string, []string, error) {
	if strings.TrimSpace(input) == "" {
		input = "."
	}
	info, err := os.Stat(input)
	if err != nil {
		return nil, nil, nil, err
	}
	if info.Mode().IsRegular() {
		chunk, err := newMergeChunk(input, outputPath)
		if err != nil {
			return nil, nil, nil, err
		}
		if chunk == nil {
			return nil, nil, nil, nil
		}
		return []mergeChunk{*chunk}, nil, nil, nil
	}
	if !info.IsDir() {
		return nil, nil, nil, fmt.Errorf("input %q must be a file or directory", input)
	}
	entries, err := os.ReadDir(input)
	if err != nil {
		return nil, nil, nil, err
	}
	splitDirs := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !isSplitDirName(entry.Name()) {
			continue
		}
		splitDirs = append(splitDirs, filepath.Join(input, entry.Name()))
	}
	sort.Slice(splitDirs, func(i, j int) bool {
		return naturalLess(filepath.Base(splitDirs[i]), filepath.Base(splitDirs[j]))
	})
	if len(splitDirs) > 0 {
		chunks := make([]mergeChunk, 0)
		cleanupDirs := make([]string, 0, len(splitDirs))
		cleanupFiles := make([]string, 0)
		for _, dir := range splitDirs {
			dirChunks, dirCleanupFiles, err := discoverMergeDirFiles(dir, outputPath)
			if err != nil {
				return nil, nil, nil, err
			}
			chunks = append(chunks, dirChunks...)
			cleanupDirs = append(cleanupDirs, dir)
			cleanupFiles = append(cleanupFiles, dirCleanupFiles...)
		}
		return chunks, cleanupDirs, cleanupFiles, nil
	}
	chunks, cleanupFiles, err := discoverMergeDirFiles(input, outputPath)
	if err != nil {
		return nil, nil, nil, err
	}
	return chunks, nil, cleanupFiles, nil
}

func discoverMergeDirFiles(dir, outputPath string) ([]mergeChunk, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	manifestNames := make([]string, 0)
	fileNames := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isMergeManifestName(entry.Name()) {
			manifestNames = append(manifestNames, entry.Name())
			continue
		}
		fileNames = append(fileNames, entry.Name())
	}
	sort.Slice(manifestNames, func(i, j int) bool {
		return naturalLess(manifestNames[i], manifestNames[j])
	})
	if len(manifestNames) > 0 {
		chunks := make([]mergeChunk, 0)
		cleanupFiles := make([]string, 0, len(manifestNames))
		for _, name := range manifestNames {
			manifestPath := filepath.Join(dir, name)
			manifestChunks, err := discoverMergeChunksFromManifest(manifestPath, dir, outputPath)
			if err != nil {
				return nil, nil, err
			}
			chunks = append(chunks, manifestChunks...)
			cleanupFiles = append(cleanupFiles, manifestPath)
		}
		return chunks, cleanupFiles, nil
	}
	filteredNames := make([]string, 0, len(fileNames))
	for _, name := range fileNames {
		if isSupportedMergeChunkName(name) {
			filteredNames = append(filteredNames, name)
		}
	}
	sort.Slice(filteredNames, func(i, j int) bool {
		return naturalLess(filteredNames[i], filteredNames[j])
	})
	chunks := make([]mergeChunk, 0, len(filteredNames))
	for _, name := range filteredNames {
		chunk, err := newMergeChunk(filepath.Join(dir, name), outputPath)
		if err != nil {
			return nil, nil, err
		}
		if chunk == nil {
			continue
		}
		chunks = append(chunks, *chunk)
	}
	return chunks, nil, nil
}

func discoverMergeChunksFromManifest(manifestPath, dir, outputPath string) ([]mergeChunk, error) {
	manifest, err := loadMergeManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	if manifest.FirstChunkInThisSplitPart < 1 || manifest.LastChunkInThisSplitPart < manifest.FirstChunkInThisSplitPart {
		return nil, fmt.Errorf("manifest %s has an invalid chunk range", manifestPath)
	}
	chunks := make([]mergeChunk, 0, manifest.LastChunkInThisSplitPart-manifest.FirstChunkInThisSplitPart+1)
	for index := manifest.FirstChunkInThisSplitPart; index <= manifest.LastChunkInThisSplitPart; index++ {
		name, err := renderMergeManifestChunkName(manifest.ChunkFilenamePattern, index)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", manifestPath, err)
		}
		chunk, err := newMergeChunk(filepath.Join(dir, name), outputPath)
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			return nil, fmt.Errorf("manifest chunk %s is missing", filepath.Join(dir, name))
		}
		chunks = append(chunks, *chunk)
	}
	return chunks, nil
}

func loadMergeManifest(path string) (mergeManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return mergeManifest{}, err
	}
	manifest := mergeManifest{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return mergeManifest{}, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if strings.TrimSpace(manifest.ChunkFilenamePattern) == "" {
		return mergeManifest{}, fmt.Errorf("manifest %s does not include chunk_filename_pattern", path)
	}
	return manifest, nil
}

func renderMergeManifestChunkName(pattern string, index int) (string, error) {
	start := strings.Index(pattern, "#")
	if start < 0 {
		return "", fmt.Errorf("manifest chunk filename pattern %q does not contain a numeric placeholder", pattern)
	}
	end := start
	for end < len(pattern) && pattern[end] == '#' {
		end++
	}
	if strings.Contains(pattern[end:], "#") {
		return "", fmt.Errorf("manifest chunk filename pattern %q contains multiple numeric placeholders", pattern)
	}
	return pattern[:start] + fmt.Sprintf("%0*d", end-start, index) + pattern[end:], nil
}

func newMergeChunk(path, outputPath string) (*mergeChunk, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if sameFilePath(absolutePath, outputPath) {
		return nil, nil
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	return &mergeChunk{path: absolutePath, size: info.Size()}, nil
}

func mergeModeName(req MergeRequest) string {
	if req.Unsafe {
		return "unsafe"
	}
	return "safe"
}

func completeMergeDryRun(plan mergePlan, runtime *downloadRuntime, task *downloadTask, req MergeRequest) error {
	if task != nil {
		task.MarkStarted()
		task.SetState("dry run")
		task.SetDetail("dry run")
		task.SetDownloaded(plan.totalSize)
		task.MarkCompleted()
	}
	chunkPaths := make([]string, 0, len(plan.chunks))
	for _, chunk := range plan.chunks {
		chunkPaths = append(chunkPaths, chunk.path)
	}
	mergeReport(runtime, "merge_dry_run", map[string]any{
		"mode":          mergeModeName(req),
		"delete_chunks": req.DeleteChunks,
		"output":        plan.outputPath,
		"chunk_count":   len(plan.chunks),
		"total_size":    plan.totalSize,
		"chunks":        chunkPaths,
	})
	if req.JSONOutput {
		return nil
	}
	for _, chunk := range plan.chunks {
		fmt.Println(displayMergeChunkPath(chunk.path))
	}
	return nil
}

func displayMergeChunkPath(path string) string {
	rel, err := filepath.Rel(".", path)
	if err == nil && rel != "" && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return path
}

func openMergeOutput(path string, overwrite bool) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return nil, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	return os.OpenFile(path, flags, 0o666)
}

func openMergeOutputTarget(path string, overwrite, safeMode bool) (mergeOutputTarget, error) {
	if !safeMode {
		file, err := openMergeOutput(path, overwrite)
		if err != nil {
			return mergeOutputTarget{}, err
		}
		return mergeOutputTarget{file: file, path: path, safeMode: false}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return mergeOutputTarget{}, err
	}
	if _, err := os.Stat(path); err == nil && !overwrite {
		return mergeOutputTarget{}, fmt.Errorf("open %s: file exists", path)
	} else if err != nil && !os.IsNotExist(err) {
		return mergeOutputTarget{}, err
	}
	tempPath := mergeTempOutputPath(path)
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o666)
	if err != nil {
		return mergeOutputTarget{}, err
	}
	return mergeOutputTarget{file: file, path: path, tempPath: tempPath, safeMode: true}, nil
}

func mergeTempOutputPath(path string) string {
	base := fmt.Sprintf("%s.gdrivedl-merge-partial", path)
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s.%d", base, index)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func finalizeMergeOutput(target *mergeOutputTarget, overwrite bool, task *downloadTask) error {
	if target == nil || target.file == nil {
		return fmt.Errorf("merge output is not open")
	}
	if err := target.file.Close(); err != nil {
		return err
	}
	target.file = nil
	if !target.safeMode {
		return nil
	}
	if task != nil {
		task.SetState("renaming output")
	}
	if overwrite {
		if err := os.Remove(target.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(target.tempPath, target.path)
}

func cleanupMergeChunks(chunks []mergeChunk, runtime *downloadRuntime, verbosity int) error {
	for _, chunk := range chunks {
		if err := os.Remove(chunk.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		mergeLog(runtime, verbosity, 2, "merge_chunk_deleted", "[merge] deleted chunk=%s\n", chunk.path)
	}
	return nil
}

func cleanupMergePaths(paths []string, runtime *downloadRuntime, verbosity int) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		mergeLog(runtime, verbosity, 2, "merge_cleanup_file_deleted", "[merge] deleted cleanup file=%s\n", path)
	}
	return nil
}

func cleanupMergeDirs(dirs []string) {
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}
}

type mergeContextReadCloser struct {
	ctx context.Context
	io.ReadCloser
}

func (r *mergeContextReadCloser) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.ReadCloser.Read(p)
}

func isSplitDirName(name string) bool {
	if !strings.HasPrefix(name, "split_") {
		return false
	}
	if len(name) == len("split_") {
		return false
	}
	for _, r := range name[len("split_"):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isMergeManifestName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".manifest") || strings.HasSuffix(lower, ".manifest.json")
}

func isSupportedMergeChunkName(name string) bool {
	lower := strings.ToLower(name)
	if isMergeManifestName(lower) {
		return false
	}
	return hasNumericChunkExtension(name) ||
		hasNumericChunkSuffix(lower, ".part") ||
		hasNumericChunkSuffix(lower, ".chunk") ||
		hasNumericPrefixPartName(lower) ||
		hasChunkOfTotalPattern(lower) ||
		hasLegacyChunkPrefix(lower)
}

func hasNumericChunkExtension(name string) bool {
	ext := filepath.Ext(name)
	return len(ext) >= 4 && allDigits(ext[1:])
}

func hasNumericChunkSuffix(name, marker string) bool {
	index := strings.LastIndex(name, marker)
	if index < 0 {
		return false
	}
	digits := name[index+len(marker):]
	return digits != "" && allDigits(digits)
}

func hasNumericPrefixPartName(name string) bool {
	if !strings.HasSuffix(name, ".part") {
		return false
	}
	prefix := strings.TrimSuffix(name, ".part")
	return prefix != "" && allDigits(prefix)
}

func hasChunkOfTotalPattern(name string) bool {
	index := strings.LastIndex(name, "chunk")
	if index < 0 {
		return false
	}
	rest := name[index+len("chunk"):]
	parts := strings.SplitN(rest, "-of-", 2)
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != "" && allDigits(parts[0]) && allDigits(parts[1])
}

func hasLegacyChunkPrefix(name string) bool {
	if !strings.HasPrefix(name, "chunk") {
		return false
	}
	rest := name[len("chunk"):]
	if rest == "" {
		return false
	}
	if rest[0] == '_' || rest[0] == '-' || rest[0] == '.' {
		rest = rest[1:]
	}
	if rest == "" {
		return false
	}
	index := 0
	for index < len(rest) && allDigits(rest[index:index+1]) {
		index++
	}
	if index == 0 {
		return false
	}
	if index == len(rest) {
		return true
	}
	return rest[index] == '.' && index+1 < len(rest)
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if !isDigit(value[i]) {
			return false
		}
	}
	return true
}

func sameFilePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func naturalLess(a, b string) bool {
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		ar, br := a[ai], b[bi]
		if isDigit(ar) && isDigit(br) {
			aStart, bStart := ai, bi
			for ai < len(a) && isDigit(a[ai]) {
				ai++
			}
			for bi < len(b) && isDigit(b[bi]) {
				bi++
			}
			aDigits := strings.TrimLeft(a[aStart:ai], "0")
			bDigits := strings.TrimLeft(b[bStart:bi], "0")
			if aDigits == "" {
				aDigits = "0"
			}
			if bDigits == "" {
				bDigits = "0"
			}
			if len(aDigits) != len(bDigits) {
				return len(aDigits) < len(bDigits)
			}
			if aDigits != bDigits {
				return aDigits < bDigits
			}
			continue
		}
		lowerA, lowerB := lowerASCII(ar), lowerASCII(br)
		if lowerA != lowerB {
			return lowerA < lowerB
		}
		ai++
		bi++
	}
	if ai == len(a) && bi == len(b) {
		return a < b
	}
	return len(a) < len(b)
}

func isDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func lowerASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func mergeLog(runtime *downloadRuntime, verbosity, minVerbosity int, name, format string, args ...interface{}) {
	if verbosity < minVerbosity && (runtime == nil || !runtime.forceJSONLogs()) {
		return
	}
	message := fmt.Sprintf(format, args...)
	if runtime != nil {
		runtime.log(name, message, nil)
		return
	}
	fmt.Printf("%s", message)
}

func mergeReport(runtime *downloadRuntime, name string, fields map[string]any) {
	if runtime == nil {
		return
	}
	runtime.report(name, fields)
}
