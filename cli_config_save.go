package gdrivedl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func saveScanReportYAML(path string, report scanReport) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("scan save path is required")
	}
	values, err := loadYAMLCLIConfigPath(path)
	if err != nil {
		return err
	}
	mergeScanReportIntoYAMLConfig(&values, report)
	return writeYAMLCLIConfigPath(path, values)
}

func loadYAMLCLIConfigPath(path string) (yamlCLIConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return yamlCLIConfig{}, nil
		}
		return yamlCLIConfig{}, err
	}
	defer file.Close()
	values, err := parseYAMLCLIConfig(file)
	if err != nil {
		return yamlCLIConfig{}, fmt.Errorf("parse YAML config %s: %w", path, err)
	}
	return values, nil
}

func mergeScanReportIntoYAMLConfig(values *yamlCLIConfig, report scanReport) {
	if values == nil {
		return
	}
	savedResolveTo := report.ResolveToAddrs
	if len(savedResolveTo) == 0 {
		savedResolveTo = report.DialAccessibleIPs
	}
	if len(savedResolveTo) > 0 {
		values.Scan.ResolveTo = stringPointer(strings.Join(savedResolveTo, ","))
	}
	if len(report.FrontingTargets) > 0 {
		joinedTargets := strings.Join(report.FrontingTargets, ",")
		values.Scan.FrontingTarget = stringPointer(joinedTargets)
		values.Transport.FrontingTarget = stringPointer(joinedTargets)
		values.Transport.FrontingEnable = boolPointer(true)
	} else {
		values.Transport.FrontingEnable = boolPointer(false)
		values.Transport.FrontingTarget = nil
	}
	if len(report.FrontingSNIs) > 0 {
		values.Scan.FrontingSNI = stringPointer(strings.Join(report.FrontingSNIs, ","))
		values.Transport.FrontingSNI = stringPointer(report.FrontingSNIs[0])
	} else {
		values.Transport.FrontingSNI = nil
	}
	if len(report.UTLSProfiles) > 0 {
		joinedProfiles := strings.Join(report.UTLSProfiles, ",")
		values.Scan.UTLSProfile = stringPointer(joinedProfiles)
		values.Transport.UTLSProfile = stringPointer(joinedProfiles)
	}
	if len(report.ResolveToAddrs) > 0 {
		values.Transport.ResolveTo = stringPointer(strings.Join(report.ResolveToAddrs, ","))
	} else if len(report.FrontingTargets) == 0 && len(report.DirectProfiles) > 0 {
		values.Transport.ResolveTo = nil
	}
	if len(report.DirectProfiles) > 0 {
		values.Transport.UTLSProfile = stringPointer(strings.Join(report.DirectProfiles, ","))
		values.Transport.FrontingEnable = boolPointer(false)
		values.Transport.FrontingTarget = nil
		values.Transport.FrontingSNI = nil
		values.Transport.ResolveTo = nil
	}
	if len(report.DirectProfiles) == 0 && len(report.FrontingTargets) > 0 && len(report.ResolveToAddrs) > 0 {
		values.Transport.FrontingEnable = boolPointer(true)
	}
}

func writeYAMLCLIConfigPath(path string, values yamlCLIConfig) error {
	directory := filepath.Dir(path)
	if directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	file, err := os.CreateTemp(directory, ".gdrivedl-config-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := file.WriteString(formatYAMLCLIConfig(values)); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func formatYAMLCLIConfig(values yamlCLIConfig) string {
	sections := [][]string{
		formatDefaultsYAMLSection(values.Defaults),
		formatTransportYAMLSection("transport", values.Transport),
		formatGetYAMLSection(values.Get),
		formatScanYAMLSection(values.Scan),
		formatTestYAMLSection(values.Test),
		formatMergeYAMLSection(values.Merge),
	}
	var builder strings.Builder
	first := true
	for _, section := range sections {
		if len(section) == 0 {
			continue
		}
		if !first {
			builder.WriteByte('\n')
		}
		first = false
		for _, line := range section {
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func formatDefaultsYAMLSection(values yamlDefaultsFileConfig) []string {
	lines := make([]string, 0, 2)
	appendBoolYAMLLine(&lines, "json", values.JSONOutput)
	return wrapYAMLSection("defaults", lines)
}

func formatTransportYAMLSection(name string, values yamlTransportFileConfig) []string {
	lines := make([]string, 0, 16)
	appendBoolYAMLLine(&lines, "dump-request", values.DumpRequest)
	appendBoolYAMLLine(&lines, "dump-response", values.DumpResponse)
	appendBoolYAMLLine(&lines, "fronting-enable", values.FrontingEnable)
	appendStringYAMLLine(&lines, "fronting-sni", values.FrontingSNI)
	appendStringYAMLLine(&lines, "fronting-target", values.FrontingTarget)
	appendBoolYAMLLine(&lines, "force-http1", values.ForceHTTP1)
	appendBoolYAMLLine(&lines, "prefer-http2", values.PreferHTTP2)
	appendStringYAMLLine(&lines, "proxy", values.Proxy)
	appendStringYAMLLine(&lines, "request-delay", values.RequestDelay)
	appendStringYAMLLine(&lines, "resolve-to", values.ResolveTo)
	appendIntYAMLLine(&lines, "retry-count", values.RetryCount)
	appendBoolYAMLLine(&lines, "share-http2-connection", values.ShareHTTP2Conn)
	appendStringYAMLLine(&lines, "timeout", values.Timeout)
	appendStringYAMLLine(&lines, "utls-profile", values.UTLSProfile)
	appendIntYAMLLine(&lines, "verbosity", values.Verbosity)
	appendIntYAMLLine(&lines, "scan-ip-random-count", values.ScanIPRandomCount)
	appendStringYAMLLine(&lines, "scan-domain-list", values.ScanDomainList)
	appendStringYAMLLine(&lines, "scan-ip-list", values.ScanIPList)
	return wrapYAMLSection(name, lines)
}

func formatGetYAMLSection(values yamlGetFileConfig) []string {
	lines := make([]string, 0, 28)
	lines = append(lines, formatTransportYAMLSectionLines(values.yamlTransportFileConfig)...)
	appendStringYAMLLine(&lines, "api-key", values.APIKey)
	appendBoolYAMLLine(&lines, "completion-report", values.CompletionReport)
	appendBoolYAMLLine(&lines, "dry-run", values.DryRun)
	appendBoolYAMLLine(&lines, "progress", values.EnableProgress)
	appendBoolYAMLLine(&lines, "exit-report", values.ExitReport)
	appendStringYAMLLine(&lines, "extension", values.Extension)
	appendStringYAMLLine(&lines, "filename", values.Filename)
	appendBoolYAMLLine(&lines, "json", values.JSONOutput)
	appendIntYAMLLine(&lines, "concurrency", values.MaxConcurrency)
	appendStringYAMLLine(&lines, "mime-type", values.MimeTypes)
	appendBoolYAMLLine(&lines, "no-top-directory", values.NotCreateTopDirectory)
	appendBoolYAMLLine(&lines, "overwrite", values.Overwrite)
	appendStringYAMLLine(&lines, "resumable-download", values.ResumableDownload)
	appendBoolYAMLLine(&lines, "file-info", values.ShowFileInfo)
	appendBoolYAMLLine(&lines, "skip", values.Skip)
	appendBoolYAMLLine(&lines, "skip-errors", values.SkipError)
	appendStringYAMLLine(&lines, "directory", values.WorkDir)
	return wrapYAMLSection("get", lines)
}

func formatScanYAMLSection(values yamlScanFileConfig) []string {
	lines := make([]string, 0, 20)
	lines = append(lines, formatTransportYAMLSectionLines(values.yamlTransportFileConfig)...)
	appendBoolYAMLLine(&lines, "json", values.JSONOutput)
	appendStringYAMLLine(&lines, "scan-mode", values.ScanMode)
	return wrapYAMLSection("scan", lines)
}

func formatTestYAMLSection(values yamlTestFileConfig) []string {
	lines := make([]string, 0, 18)
	lines = append(lines, formatTransportYAMLSectionLines(values.yamlTransportFileConfig)...)
	appendBoolYAMLLine(&lines, "json", values.JSONOutput)
	return wrapYAMLSection("test", lines)
}

func formatMergeYAMLSection(values yamlMergeFileConfig) []string {
	lines := make([]string, 0, 8)
	appendBoolYAMLLine(&lines, "delete-chunks", values.DeleteChunks)
	appendBoolYAMLLine(&lines, "exit-report", values.ExitReport)
	appendBoolYAMLLine(&lines, "json", values.JSONOutput)
	appendStringYAMLLine(&lines, "output", values.Output)
	appendBoolYAMLLine(&lines, "overwrite", values.Overwrite)
	appendBoolYAMLLine(&lines, "progress", values.Progress)
	appendBoolYAMLLine(&lines, "unsafe", values.Unsafe)
	appendIntYAMLLine(&lines, "verbosity", values.Verbosity)
	return wrapYAMLSection("merge", lines)
}

func formatTransportYAMLSectionLines(values yamlTransportFileConfig) []string {
	lines := make([]string, 0, 16)
	appendBoolYAMLLine(&lines, "dump-request", values.DumpRequest)
	appendBoolYAMLLine(&lines, "dump-response", values.DumpResponse)
	appendBoolYAMLLine(&lines, "fronting-enable", values.FrontingEnable)
	appendStringYAMLLine(&lines, "fronting-sni", values.FrontingSNI)
	appendStringYAMLLine(&lines, "fronting-target", values.FrontingTarget)
	appendBoolYAMLLine(&lines, "force-http1", values.ForceHTTP1)
	appendBoolYAMLLine(&lines, "prefer-http2", values.PreferHTTP2)
	appendStringYAMLLine(&lines, "proxy", values.Proxy)
	appendStringYAMLLine(&lines, "request-delay", values.RequestDelay)
	appendStringYAMLLine(&lines, "resolve-to", values.ResolveTo)
	appendIntYAMLLine(&lines, "retry-count", values.RetryCount)
	appendBoolYAMLLine(&lines, "share-http2-connection", values.ShareHTTP2Conn)
	appendStringYAMLLine(&lines, "timeout", values.Timeout)
	appendStringYAMLLine(&lines, "utls-profile", values.UTLSProfile)
	appendIntYAMLLine(&lines, "verbosity", values.Verbosity)
	appendIntYAMLLine(&lines, "scan-ip-random-count", values.ScanIPRandomCount)
	appendStringYAMLLine(&lines, "scan-domain-list", values.ScanDomainList)
	appendStringYAMLLine(&lines, "scan-ip-list", values.ScanIPList)
	return lines
}

func wrapYAMLSection(name string, lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	section := make([]string, 0, len(lines)+1)
	section = append(section, name+":")
	section = append(section, lines...)
	return section
}

func appendStringYAMLLine(lines *[]string, key string, value *string) {
	if value == nil {
		return
	}
	raw := *value
	if raw == "" {
		raw = "''"
	}
	*lines = append(*lines, fmt.Sprintf("  %s: %s", key, raw))
}

func appendBoolYAMLLine(lines *[]string, key string, value *bool) {
	if value == nil {
		return
	}
	*lines = append(*lines, fmt.Sprintf("  %s: %t", key, *value))
}

func appendIntYAMLLine(lines *[]string, key string, value *int) {
	if value == nil {
		return
	}
	*lines = append(*lines, fmt.Sprintf("  %s: %d", key, *value))
}
