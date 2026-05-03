package gdrivedl

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/urfave/cli"
)

type cliParseEnvironment struct {
	getenv        func(string) string
	userConfigDir func() (string, error)
	absPath       func(string) (string, error)
}

type yamlDefaultsFileConfig struct {
	JSONOutput *bool `yaml:"json"`
}

type yamlTransportFileConfig struct {
	DumpRequest       *bool   `yaml:"dump-request"`
	DumpResponse      *bool   `yaml:"dump-response"`
	FrontingEnable    *bool   `yaml:"fronting-enable"`
	FrontingSNI       *string `yaml:"fronting-sni"`
	FrontingTarget    *string `yaml:"fronting-target"`
	ForceHTTP1        *bool   `yaml:"force-http1"`
	PreferHTTP2       *bool   `yaml:"prefer-http2"`
	Proxy             *string `yaml:"proxy"`
	RoundTripTimeout  *string `yaml:"roundtrip-timeout"`
	RequestDelay      *string `yaml:"request-delay"`
	ResolveTo         *string `yaml:"resolve-to"`
	RetryCount        *int    `yaml:"retry-count"`
	ShareHTTP2Conn    *bool   `yaml:"share-http2-connection"`
	Timeout           *string `yaml:"timeout"`
	UTLSProfile       *string `yaml:"utls-profile"`
	Verbosity         *int    `yaml:"verbosity"`
	ScanIPRandomCount *int    `yaml:"scan-ip-random-count"`
	ScanDomainList    *string `yaml:"scan-domain-list"`
	ScanIPList        *string `yaml:"scan-ip-list"`
}

type yamlGetFileConfig struct {
	yamlTransportFileConfig `yaml:",inline"`
	APIKey                  *string `yaml:"api-key"`
	CompletionReport        *bool   `yaml:"completion-report"`
	DryRun                  *bool   `yaml:"dry-run"`
	EnableRedownload        *bool   `yaml:"enable-redownload"`
	EnableProgress          *bool   `yaml:"progress"`
	ExitReport              *bool   `yaml:"exit-report"`
	Extension               *string `yaml:"extension"`
	Filename                *string `yaml:"filename"`
	JSONOutput              *bool   `yaml:"json"`
	MaxConcurrency          *int    `yaml:"concurrency"`
	MimeTypes               *string `yaml:"mime-type"`
	NotCreateTopDirectory   *bool   `yaml:"no-top-directory"`
	Overwrite               *bool   `yaml:"overwrite"`
	ResumableDownload       *string `yaml:"resumable-download"`
	ShowFileInfo            *bool   `yaml:"file-info"`
	Skip                    *bool   `yaml:"skip"`
	SkipError               *bool   `yaml:"skip-errors"`
	WorkDir                 *string `yaml:"directory"`
}

type yamlScanFileConfig struct {
	yamlTransportFileConfig `yaml:",inline"`
	JSONOutput              *bool   `yaml:"json"`
	ScanConcurrency         *int    `yaml:"scan-concurrency"`
	ScanMode                *string `yaml:"scan-mode"`
}

type yamlTestFileConfig struct {
	yamlTransportFileConfig `yaml:",inline"`
	JSONOutput              *bool `yaml:"json"`
}

type yamlMergeFileConfig struct {
	DeleteChunks *bool   `yaml:"delete-chunks"`
	DryRun       *bool   `yaml:"dry-run"`
	ExitReport   *bool   `yaml:"exit-report"`
	JSONOutput   *bool   `yaml:"json"`
	Output       *string `yaml:"output"`
	Overwrite    *bool   `yaml:"overwrite"`
	Progress     *bool   `yaml:"progress"`
	Unsafe       *bool   `yaml:"unsafe"`
	Verbosity    *int    `yaml:"verbosity"`
}

type yamlCLIConfig struct {
	Defaults  yamlDefaultsFileConfig  `yaml:"defaults"`
	Transport yamlTransportFileConfig `yaml:"transport"`
	Get       yamlGetFileConfig       `yaml:"get"`
	Scan      yamlScanFileConfig      `yaml:"scan"`
	Test      yamlTestFileConfig      `yaml:"test"`
	Merge     yamlMergeFileConfig     `yaml:"merge"`
}

type loadedCLIConfig struct {
	Path   string
	Values yamlCLIConfig
}

func defaultCLIParseEnvironment() cliParseEnvironment {
	return cliParseEnvironment{
		getenv:        os.Getenv,
		userConfigDir: os.UserConfigDir,
		absPath:       filepath.Abs,
	}
}

func (env cliParseEnvironment) normalize() cliParseEnvironment {
	if env.getenv == nil {
		env.getenv = os.Getenv
	}
	if env.userConfigDir == nil {
		env.userConfigDir = os.UserConfigDir
	}
	if env.absPath == nil {
		env.absPath = filepath.Abs
	}
	return env
}

func resolveDefaultConfigPath(env cliParseEnvironment) (string, error) {
	env = env.normalize()
	if dir := strings.TrimSpace(env.getenv("XDG_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "gdrivedl.yml"), nil
	}
	if dir := strings.TrimSpace(env.getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "gdrivedl.yml"), nil
	}
	dir, err := env.userConfigDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dir) == "" {
		return "", nil
	}
	return filepath.Join(dir, "gdrivedl.yml"), nil
}

func loadCLIFileConfig(c *cli.Context, env cliParseEnvironment) (loadedCLIConfig, error) {
	env = env.normalize()
	explicit := false
	path := ""
	if c != nil && (c.IsSet("config") || c.GlobalIsSet("config")) {
		explicit = true
		path = cliString(c, "config")
		if path == "" {
			return loadedCLIConfig{}, nil
		}
	} else {
		defaultPath, err := resolveDefaultConfigPath(env)
		if err != nil {
			return loadedCLIConfig{}, err
		}
		path = defaultPath
		if path == "" {
			return loadedCLIConfig{}, nil
		}
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return loadedCLIConfig{}, nil
		}
		return loadedCLIConfig{}, err
	}
	defer file.Close()
	values, err := parseYAMLCLIConfig(file)
	if err != nil {
		return loadedCLIConfig{}, fmt.Errorf("parse YAML config %s: %w", path, err)
	}
	return loadedCLIConfig{Path: path, Values: values}, nil
}

func parseYAMLCLIConfig(reader io.Reader) (yamlCLIConfig, error) {
	var values yamlCLIConfig
	scanner := bufio.NewScanner(reader)
	section := ""
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		rawLine := scanner.Text()
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(rawLine, "\t") {
			return values, fmt.Errorf("line %d: tab indentation is not supported", lineNumber)
		}
		indent := len(rawLine) - len(strings.TrimLeft(rawLine, " "))
		key, rawValue, ok := strings.Cut(trimmed, ":")
		if !ok {
			return values, fmt.Errorf("line %d: expected key: value", lineNumber)
		}
		key = strings.TrimSpace(key)
		value := strings.TrimSpace(rawValue)
		if indent == 0 {
			if value != "" {
				return values, fmt.Errorf("line %d: top-level entries must be sections", lineNumber)
			}
			section = key
			if !isSupportedConfigSection(section) {
				return values, fmt.Errorf("line %d: unsupported config section %q", lineNumber, section)
			}
			continue
		}
		if section == "" {
			return values, fmt.Errorf("line %d: nested key %q must belong to a section", lineNumber, key)
		}
		if err := assignYAMLCLIConfigValue(&values, section, key, unquoteYAMLScalar(value), lineNumber); err != nil {
			return values, err
		}
	}
	if err := scanner.Err(); err != nil {
		return values, err
	}
	return values, nil
}

func isSupportedConfigSection(section string) bool {
	switch section {
	case "defaults", "transport", "get", "scan", "test", "merge":
		return true
	default:
		return false
	}
}

func unquoteYAMLScalar(value string) string {
	if len(value) >= 2 {
		if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func assignYAMLCLIConfigValue(values *yamlCLIConfig, section, key, raw string, lineNumber int) error {
	if values == nil {
		return fmt.Errorf("line %d: config destination is nil", lineNumber)
	}
	switch section {
	case "defaults":
		switch key {
		case "json":
			return assignBoolPointer(&values.Defaults.JSONOutput, raw, lineNumber, key)
		default:
			return fmt.Errorf("line %d: unsupported defaults key %q", lineNumber, key)
		}
	case "transport":
		return assignTransportConfigValue(&values.Transport, key, raw, lineNumber)
	case "get":
		if err := assignTransportConfigValue(&values.Get.yamlTransportFileConfig, key, raw, lineNumber); err == nil {
			return nil
		} else if !strings.Contains(err.Error(), "unsupported transport key") {
			return err
		}
		switch key {
		case "api-key", "apikey":
			values.Get.APIKey = stringPointer(raw)
		case "completion-report":
			return assignBoolPointer(&values.Get.CompletionReport, raw, lineNumber, key)
		case "dry-run":
			return assignBoolPointer(&values.Get.DryRun, raw, lineNumber, key)
		case "enable-redownload", "redownload":
			return assignBoolPointer(&values.Get.EnableRedownload, raw, lineNumber, key)
		case "progress":
			return assignBoolPointer(&values.Get.EnableProgress, raw, lineNumber, key)
		case "exit-report":
			return assignBoolPointer(&values.Get.ExitReport, raw, lineNumber, key)
		case "extension":
			values.Get.Extension = stringPointer(raw)
		case "filename":
			values.Get.Filename = stringPointer(raw)
		case "json":
			return assignBoolPointer(&values.Get.JSONOutput, raw, lineNumber, key)
		case "concurrency":
			return assignIntPointer(&values.Get.MaxConcurrency, raw, lineNumber, key)
		case "mime-type", "mimetype":
			values.Get.MimeTypes = stringPointer(raw)
		case "no-top-directory", "notcreatetopdirectory":
			return assignBoolPointer(&values.Get.NotCreateTopDirectory, raw, lineNumber, key)
		case "overwrite":
			return assignBoolPointer(&values.Get.Overwrite, raw, lineNumber, key)
		case "resumable-download", "resumabledownload":
			values.Get.ResumableDownload = stringPointer(raw)
		case "file-info", "fileinf":
			return assignBoolPointer(&values.Get.ShowFileInfo, raw, lineNumber, key)
		case "skip":
			return assignBoolPointer(&values.Get.Skip, raw, lineNumber, key)
		case "skip-errors", "skiperror":
			return assignBoolPointer(&values.Get.SkipError, raw, lineNumber, key)
		case "directory":
			values.Get.WorkDir = stringPointer(raw)
		default:
			return fmt.Errorf("line %d: unsupported get key %q", lineNumber, key)
		}
		return nil
	case "scan":
		if err := assignTransportConfigValue(&values.Scan.yamlTransportFileConfig, key, raw, lineNumber); err == nil {
			return nil
		} else if !strings.Contains(err.Error(), "unsupported transport key") {
			return err
		}
		switch key {
		case "json":
			return assignBoolPointer(&values.Scan.JSONOutput, raw, lineNumber, key)
		case "scan-concurrency":
			return assignIntPointer(&values.Scan.ScanConcurrency, raw, lineNumber, key)
		case "scan-mode":
			values.Scan.ScanMode = stringPointer(raw)
		default:
			return fmt.Errorf("line %d: unsupported scan key %q", lineNumber, key)
		}
		return nil
	case "test":
		if err := assignTransportConfigValue(&values.Test.yamlTransportFileConfig, key, raw, lineNumber); err == nil {
			return nil
		} else if !strings.Contains(err.Error(), "unsupported transport key") {
			return err
		}
		switch key {
		case "json":
			return assignBoolPointer(&values.Test.JSONOutput, raw, lineNumber, key)
		default:
			return fmt.Errorf("line %d: unsupported test key %q", lineNumber, key)
		}
	case "merge":
		switch key {
		case "delete-chunks":
			return assignBoolPointer(&values.Merge.DeleteChunks, raw, lineNumber, key)
		case "dry-run":
			return assignBoolPointer(&values.Merge.DryRun, raw, lineNumber, key)
		case "exit-report":
			return assignBoolPointer(&values.Merge.ExitReport, raw, lineNumber, key)
		case "json":
			return assignBoolPointer(&values.Merge.JSONOutput, raw, lineNumber, key)
		case "output":
			values.Merge.Output = stringPointer(raw)
		case "overwrite":
			return assignBoolPointer(&values.Merge.Overwrite, raw, lineNumber, key)
		case "progress":
			return assignBoolPointer(&values.Merge.Progress, raw, lineNumber, key)
		case "unsafe":
			return assignBoolPointer(&values.Merge.Unsafe, raw, lineNumber, key)
		case "verbosity":
			return assignIntPointer(&values.Merge.Verbosity, raw, lineNumber, key)
		default:
			return fmt.Errorf("line %d: unsupported merge key %q", lineNumber, key)
		}
	}
	return fmt.Errorf("line %d: unsupported config section %q", lineNumber, section)
}

func assignTransportConfigValue(values *yamlTransportFileConfig, key, raw string, lineNumber int) error {
	if values == nil {
		return fmt.Errorf("line %d: transport config destination is nil", lineNumber)
	}
	switch key {
	case "dump-request":
		return assignBoolPointer(&values.DumpRequest, raw, lineNumber, key)
	case "dump-response":
		return assignBoolPointer(&values.DumpResponse, raw, lineNumber, key)
	case "fronting-enable":
		return assignBoolPointer(&values.FrontingEnable, raw, lineNumber, key)
	case "fronting-sni":
		values.FrontingSNI = stringPointer(raw)
	case "fronting-target":
		values.FrontingTarget = stringPointer(raw)
	case "force-http1":
		return assignBoolPointer(&values.ForceHTTP1, raw, lineNumber, key)
	case "prefer-http2":
		return assignBoolPointer(&values.PreferHTTP2, raw, lineNumber, key)
	case "proxy":
		values.Proxy = stringPointer(raw)
	case "roundtrip-timeout":
		values.RoundTripTimeout = stringPointer(raw)
	case "request-delay":
		values.RequestDelay = stringPointer(raw)
	case "resolve-to":
		values.ResolveTo = stringPointer(raw)
	case "retry-count":
		return assignIntPointer(&values.RetryCount, raw, lineNumber, key)
	case "share-http2-connection":
		return assignBoolPointer(&values.ShareHTTP2Conn, raw, lineNumber, key)
	case "timeout":
		values.Timeout = stringPointer(raw)
	case "utls-profile":
		values.UTLSProfile = stringPointer(raw)
	case "verbosity":
		return assignIntPointer(&values.Verbosity, raw, lineNumber, key)
	case "scan-ip-random-count":
		return assignIntPointer(&values.ScanIPRandomCount, raw, lineNumber, key)
	case "scan-domain-list":
		values.ScanDomainList = stringPointer(raw)
	case "scan-ip-list":
		values.ScanIPList = stringPointer(raw)
	default:
		return fmt.Errorf("line %d: unsupported transport key %q", lineNumber, key)
	}
	return nil
}

func assignBoolPointer(target **bool, raw string, lineNumber int, key string) error {
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("line %d: %s must be a boolean", lineNumber, key)
	}
	*target = boolPointer(value)
	return nil
}

func assignIntPointer(target **int, raw string, lineNumber int, key string) error {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("line %d: %s must be an integer", lineNumber, key)
	}
	*target = intPointer(value)
	return nil
}

func stringPointer(value string) *string {
	copyValue := value
	return &copyValue
}

func boolPointer(value bool) *bool {
	copyValue := value
	return &copyValue
}

func intPointer(value int) *int {
	copyValue := value
	return &copyValue
}

func resolveStringOption(c *cli.Context, name string, sources ...*string) (string, bool) {
	if cliFlagIsSet(c, name) {
		return cliString(c, name), true
	}
	for _, source := range sources {
		if source != nil {
			return *source, true
		}
	}
	return "", false
}

func resolveBoolOption(c *cli.Context, name string, sources ...*bool) (bool, bool) {
	if cliFlagIsSet(c, name) {
		return cliBool(c, name), true
	}
	for _, source := range sources {
		if source != nil {
			return *source, true
		}
	}
	return false, false
}

func resolveBoolOptionWithNegative(c *cli.Context, positiveName, negativeName string, sources ...*bool) (bool, bool, error) {
	positiveSet := cliFlagIsSet(c, positiveName)
	negativeSet := negativeName != "" && cliFlagIsSet(c, negativeName)
	if positiveSet && negativeSet {
		return false, false, fmt.Errorf("please use either '--%s' or '--%s'", positiveName, negativeName)
	}
	if positiveSet {
		return true, true, nil
	}
	if negativeSet {
		return false, true, nil
	}
	for _, source := range sources {
		if source != nil {
			return *source, true, nil
		}
	}
	return false, false, nil
}

func resolveIntOption(c *cli.Context, name string, sources ...*int) (int, bool) {
	if cliFlagIsSet(c, name) {
		return cliInt(c, name), true
	}
	for _, source := range sources {
		if source != nil {
			return *source, true
		}
	}
	return 0, false
}

func resolveStringSliceOption(c *cli.Context, name string, sources ...*string) ([]string, bool) {
	if cliFlagIsSet(c, name) {
		return splitCommaSeparated(cliStringSlice(c, name)), true
	}
	for _, source := range sources {
		if source != nil {
			return splitCommaSeparated([]string{*source}), true
		}
	}
	return nil, false
}

func resolveJSONOutputOption(c *cli.Context, defaults *bool, command *bool) (bool, error) {
	value, _, err := resolveBoolOptionWithNegative(c, "json", "no-json", command, defaults)
	if err != nil {
		return false, err
	}
	return value, nil
}

func resolveTransportOptionFlags(c *cli.Context, shared yamlTransportFileConfig, command yamlTransportFileConfig) (transportOptionFlags, error) {
	proxy, _ := resolveStringOption(c, "proxy", command.Proxy, shared.Proxy)
	timeout, _ := resolveStringOption(c, "timeout", command.Timeout, shared.Timeout)
	roundTripTimeout, _ := resolveStringOption(c, "roundtrip-timeout", command.RoundTripTimeout, shared.RoundTripTimeout)
	requestDelay, _ := resolveStringOption(c, "request-delay", command.RequestDelay, shared.RequestDelay)
	retryCount, _ := resolveIntOption(c, "retry-count", command.RetryCount, shared.RetryCount)
	verbosity, _ := resolveIntOption(c, "verbosity", command.Verbosity, shared.Verbosity)
	dumpRequest, _, err := resolveBoolOptionWithNegative(c, "dump-request", "no-dump-request", command.DumpRequest, shared.DumpRequest)
	if err != nil {
		return transportOptionFlags{}, err
	}
	dumpResponse, _, err := resolveBoolOptionWithNegative(c, "dump-response", "no-dump-response", command.DumpResponse, shared.DumpResponse)
	if err != nil {
		return transportOptionFlags{}, err
	}
	resolveTo, _ := resolveStringOption(c, "resolve-to", command.ResolveTo, shared.ResolveTo)
	utlsProfile, _ := resolveStringOption(c, "utls-profile", command.UTLSProfile, shared.UTLSProfile)
	preferHTTP2, _, err := resolveBoolOptionWithNegative(c, "prefer-http2", "no-prefer-http2", command.PreferHTTP2, shared.PreferHTTP2)
	if err != nil {
		return transportOptionFlags{}, err
	}
	forceHTTP1, _, err := resolveBoolOptionWithNegative(c, "force-http1", "no-force-http1", command.ForceHTTP1, shared.ForceHTTP1)
	if err != nil {
		return transportOptionFlags{}, err
	}
	shareHTTP2Conn, _, err := resolveBoolOptionWithNegative(c, "share-http2-connection", "no-share-http2-connection", command.ShareHTTP2Conn, shared.ShareHTTP2Conn)
	if err != nil {
		return transportOptionFlags{}, err
	}
	frontingEnable, _, err := resolveBoolOptionWithNegative(c, "fronting-enable", "no-fronting", command.FrontingEnable, shared.FrontingEnable)
	if err != nil {
		return transportOptionFlags{}, err
	}
	frontingSNI, _ := resolveStringOption(c, "fronting-sni", command.FrontingSNI, shared.FrontingSNI)
	frontingTarget, _ := resolveStringOption(c, "fronting-target", command.FrontingTarget, shared.FrontingTarget)
	if cliFlagIsSet(c, "no-fronting") {
		if !cliFlagIsSet(c, "fronting-sni") {
			frontingSNI = ""
		}
		if !cliFlagIsSet(c, "fronting-target") {
			frontingTarget = ""
		}
	}
	scanDomainList, _ := resolveStringOption(c, "scan-domain-list", command.ScanDomainList, shared.ScanDomainList)
	scanIPList, _ := resolveStringOption(c, "scan-ip-list", command.ScanIPList, shared.ScanIPList)
	scanIPRandomCount, _ := resolveIntOption(c, "scan-ip-random-count", command.ScanIPRandomCount, shared.ScanIPRandomCount)
	return transportOptionFlags{
		DumpRequest:       dumpRequest,
		DumpResponse:      dumpResponse,
		FrontingEnable:    frontingEnable,
		FrontingSNI:       frontingSNI,
		FrontingTarget:    frontingTarget,
		ForceHTTP1:        forceHTTP1,
		PreferHTTP2:       preferHTTP2,
		Proxy:             proxy,
		RoundTripTimeout:  roundTripTimeout,
		RequestDelay:      requestDelay,
		ResolveTo:         resolveTo,
		RetryCount:        retryCount,
		ShareHTTP2Conn:    shareHTTP2Conn,
		Timeout:           timeout,
		UTLSProfile:       utlsProfile,
		Verbosity:         verbosity,
		ScanDomainList:    scanDomainList,
		ScanIPList:        scanIPList,
		ScanIPRandomCount: scanIPRandomCount,
	}, nil
}

func buildFrontingConfig(enabled bool, targetRaw, sniRaw string) (frontingConfig, error) {
	targets := splitCommaSeparated([]string{targetRaw})
	sni := strings.TrimSpace(sniRaw)
	if !enabled {
		if len(targets) > 0 || sni != "" {
			return frontingConfig{}, fmt.Errorf("fronting options require --fronting-enable")
		}
		return frontingConfig{}, nil
	}
	if len(targets) == 0 {
		return frontingConfig{}, fmt.Errorf("--fronting-target is required when --fronting-enable is set")
	}
	normalizedTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		normalizedTarget, err := parseHostnameValue(target, "--fronting-target")
		if err != nil {
			return frontingConfig{}, err
		}
		normalizedTargets = append(normalizedTargets, normalizedTarget)
	}
	target := normalizedTargets[0]
	explicitSNI := sni != ""
	if sni == "" {
		sni = target
	} else {
		var err error
		sni, err = parseHostnameValue(sni, "--fronting-sni")
		if err != nil {
			return frontingConfig{}, err
		}
	}
	return frontingConfig{Enable: true, SNI: sni, Target: target, Targets: normalizedTargets, explicitSNI: explicitSNI}, nil
}

func buildTransportConfigFromOptions(options transportOptionFlags) (transportConfig, error) {
	preferHTTP2 := options.PreferHTTP2
	forceHTTP1 := options.ForceHTTP1
	shareHTTP2Conn := options.ShareHTTP2Conn
	if preferHTTP2 && forceHTTP1 {
		return transportConfig{}, fmt.Errorf("--prefer-http2 cannot be used with --force-http1")
	}
	if shareHTTP2Conn && forceHTTP1 {
		return transportConfig{}, fmt.Errorf("--share-http2-connection cannot be used with --force-http1")
	}
	proxyURL, err := parseProxyURL(options.Proxy)
	if err != nil {
		return transportConfig{}, err
	}
	fronting, err := buildFrontingConfig(options.FrontingEnable, options.FrontingTarget, options.FrontingSNI)
	if err != nil {
		return transportConfig{}, err
	}
	resolveToAddrs, err := parseResolveToList(options.ResolveTo)
	if err != nil {
		return transportConfig{}, err
	}
	resolveTo := ""
	if len(resolveToAddrs) > 0 {
		resolveTo = resolveToAddrs[0]
	}
	profiles, err := parseUTLSProfileList(options.UTLSProfile)
	if err != nil {
		return transportConfig{}, err
	}
	timeout, err := parseTimeout(options.Timeout)
	if err != nil {
		return transportConfig{}, err
	}
	roundTripTimeout, err := parseFlexibleDuration(options.RoundTripTimeout, "--roundtrip-timeout")
	if err != nil {
		return transportConfig{}, err
	}
	requestDelay, err := parseFlexibleDuration(options.RequestDelay, "--request-delay")
	if err != nil {
		return transportConfig{}, err
	}
	retryCount, err := parseRetryCount(options.RetryCount)
	if err != nil {
		return transportConfig{}, err
	}
	verbosity, err := parseVerbosity(options.Verbosity)
	if err != nil {
		return transportConfig{}, err
	}
	scanIPRandomCount, err := parseScanIPRandomCount(options.ScanIPRandomCount)
	if err != nil {
		return transportConfig{}, err
	}
	return transportConfig{
		DumpRequest:       options.DumpRequest,
		DumpResponse:      options.DumpResponse,
		Fronting:          fronting,
		ForceHTTP1:        forceHTTP1,
		PreferHTTP2:       preferHTTP2,
		Proxy:             proxyURL,
		RoundTripTimeout:  roundTripTimeout,
		RetryCount:        retryCount,
		RequestDelay:      requestDelay,
		ResolveTo:         resolveTo,
		ResolveToAddrs:    resolveToAddrs,
		Scan:              false,
		ScanIPRandomCount: scanIPRandomCount,
		ShareHTTP2Conn:    shareHTTP2Conn,
		Timeout:           timeout,
		UTLSProfile:       profiles[0].id,
		UTLSProfileName:   profiles[0].name,
		UTLSProfiles:      profiles,
		Verbosity:         verbosity,
		sharedState:       newTransportSharedState(),
	}, nil
}

func buildScanTransportConfigFromOptions(options transportOptionFlags) (transportConfig, error) {
	scanOptions := options
	scanOptions.FrontingEnable = false
	scanOptions.FrontingSNI = ""
	scanOptions.FrontingTarget = ""
	scanOptions.ResolveTo = ""
	return buildTransportConfigFromOptions(scanOptions)
}

func parseGetCommandConfigWithEnv(c *cli.Context, env cliParseEnvironment) (*config, error) {
	env = env.normalize()
	loaded, err := loadCLIFileConfig(c, env)
	if err != nil {
		return nil, err
	}
	transportOptions, err := resolveTransportOptionFlags(c, loaded.Values.Transport, loaded.Values.Get.yamlTransportFileConfig)
	if err != nil {
		return nil, err
	}
	transport, err := buildTransportConfigFromOptions(transportOptions)
	if err != nil {
		return nil, err
	}
	concurrency, concurrencySet := resolveIntOption(c, "concurrency", loaded.Values.Get.MaxConcurrency)
	if !concurrencySet {
		concurrency = 1
	}
	if concurrency < 1 {
		return nil, fmt.Errorf("--concurrency must be greater than 0")
	}
	url, _ := resolveStringOption(c, "url")
	urlList, _ := resolveStringOption(c, "url-list")
	extension, _ := resolveStringOption(c, "extension", loaded.Values.Get.Extension)
	filename, _ := resolveStringOption(c, "filename", loaded.Values.Get.Filename)
	mimeTypes, _ := resolveStringSliceOption(c, "mime-type", loaded.Values.Get.MimeTypes)
	resumableDownload, _ := resolveStringOption(c, "resumable-download", loaded.Values.Get.ResumableDownload)
	dryRun, _, err := resolveBoolOptionWithNegative(c, "dry-run", "no-dry-run", loaded.Values.Get.DryRun)
	if err != nil {
		return nil, err
	}
	enableRedownload, _, err := resolveBoolOptionWithNegative(c, "enable-redownload", "no-enable-redownload", loaded.Values.Get.EnableRedownload)
	if err != nil {
		return nil, err
	}
	enableProgress, _, err := resolveBoolOptionWithNegative(c, "progress", "no-progress", loaded.Values.Get.EnableProgress)
	if err != nil {
		return nil, err
	}
	exitReport, _, err := resolveBoolOptionWithNegative(c, "exit-report", "no-exit-report", loaded.Values.Get.ExitReport)
	if err != nil {
		return nil, err
	}
	completionReport, _, err := resolveBoolOptionWithNegative(c, "completion-report", "no-completion-report", loaded.Values.Get.CompletionReport)
	if err != nil {
		return nil, err
	}
	jsonOutput, err := resolveJSONOutputOption(c, loaded.Values.Defaults.JSONOutput, loaded.Values.Get.JSONOutput)
	if err != nil {
		return nil, err
	}
	overwrite, _, err := resolveBoolOptionWithNegative(c, "overwrite", "no-overwrite", loaded.Values.Get.Overwrite)
	if err != nil {
		return nil, err
	}
	skip, _, err := resolveBoolOptionWithNegative(c, "skip", "no-skip", loaded.Values.Get.Skip)
	if err != nil {
		return nil, err
	}
	showFileInfo, _, err := resolveBoolOptionWithNegative(c, "file-info", "no-file-info", loaded.Values.Get.ShowFileInfo)
	if err != nil {
		return nil, err
	}
	notCreateTopDirectory, _, err := resolveBoolOptionWithNegative(c, "no-top-directory", "create-top-directory", loaded.Values.Get.NotCreateTopDirectory)
	if err != nil {
		return nil, err
	}
	skipError, _, err := resolveBoolOptionWithNegative(c, "skip-errors", "no-skip-errors", loaded.Values.Get.SkipError)
	if err != nil {
		return nil, err
	}
	disp := cliFlagIsSet(c, "no-progress")
	workDir, workDirSet := resolveStringOption(c, "directory", loaded.Values.Get.WorkDir)
	if !workDirSet {
		workDir, err = env.absPath(".")
		if err != nil {
			return nil, err
		}
	}
	apiKey, apiKeySet := resolveStringOption(c, "api-key", loaded.Values.Get.APIKey)
	if !apiKeySet {
		apiKey = strings.TrimSpace(env.getenv(envval))
		if apiKey == "" {
			apiKey = strings.TrimSpace(env.getenv(legacyEnvval))
		}
	}
	return &config{
		APIKey:                strings.TrimSpace(apiKey),
		CompletionReport:      completionReport,
		Disp:                  disp,
		DryRun:                dryRun,
		EnableRedownload:      enableRedownload,
		EnableProgress:        enableProgress,
		Ext:                   extension,
		ExitReport:            exitReport,
		Filename:              filename,
		InputtedMimeType:      mimeTypes,
		JSONOutput:            jsonOutput,
		MaxConcurrency:        concurrency,
		Notcreatetopdirectory: notCreateTopDirectory,
		OverWrite:             overwrite,
		Resumabledownload:     resumableDownload,
		ShowFileInf:           showFileInfo,
		Skip:                  skip,
		SkipError:             skipError,
		Transport:             transport,
		URL:                   url,
		URLList:               urlList,
		WorkDir:               workDir,
	}, nil
}

func parseScanCommandOptionsWithEnv(c *cli.Context, env cliParseEnvironment) (scanCommandOptions, error) {
	env = env.normalize()
	loaded, err := loadCLIFileConfig(c, env)
	if err != nil {
		return scanCommandOptions{}, err
	}
	options := scanCommandOptions{}
	options.JSONOutput, err = resolveJSONOutputOption(c, loaded.Values.Defaults.JSONOutput, loaded.Values.Scan.JSONOutput)
	if err != nil {
		return scanCommandOptions{}, err
	}
	options.transportOptionFlags, err = resolveTransportOptionFlags(c, loaded.Values.Transport, loaded.Values.Scan.yamlTransportFileConfig)
	if err != nil {
		return scanCommandOptions{}, err
	}
	scanConcurrencyValue, _ := resolveIntOption(c, "scan-concurrency", loaded.Values.Scan.ScanConcurrency)
	options.ScanConcurrency, err = parseScanConcurrency(scanConcurrencyValue)
	if err != nil {
		return scanCommandOptions{}, err
	}
	scanModeValue, _ := resolveStringOption(c, "scan-mode", loaded.Values.Scan.ScanMode)
	options.ScanMode, err = parseScanMode(scanModeValue)
	if err != nil {
		return scanCommandOptions{}, err
	}
	options.SavePath, _ = resolveStringOption(c, "save")
	return options, nil
}

func parseTestCommandOptionsWithEnv(c *cli.Context, env cliParseEnvironment) (testCommandOptions, error) {
	env = env.normalize()
	loaded, err := loadCLIFileConfig(c, env)
	if err != nil {
		return testCommandOptions{}, err
	}
	options := testCommandOptions{}
	options.JSONOutput, err = resolveJSONOutputOption(c, loaded.Values.Defaults.JSONOutput, loaded.Values.Test.JSONOutput)
	if err != nil {
		return testCommandOptions{}, err
	}
	options.transportOptionFlags, err = resolveTransportOptionFlags(c, loaded.Values.Transport, loaded.Values.Test.yamlTransportFileConfig)
	if err != nil {
		return testCommandOptions{}, err
	}
	return options, nil
}

func parseMergeCommandOptionsWithEnv(c *cli.Context, env cliParseEnvironment) (mergeCommandOptions, error) {
	env = env.normalize()
	loaded, err := loadCLIFileConfig(c, env)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options := mergeCommandOptions{}
	options.JSONOutput, err = resolveJSONOutputOption(c, loaded.Values.Defaults.JSONOutput, loaded.Values.Merge.JSONOutput)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options.DeleteChunks, _, err = resolveBoolOptionWithNegative(c, "delete-chunks", "keep-chunks", loaded.Values.Merge.DeleteChunks)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options.DryRun, _, err = resolveBoolOptionWithNegative(c, "dry-run", "no-dry-run", loaded.Values.Merge.DryRun)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options.ExitReport, _, err = resolveBoolOptionWithNegative(c, "exit-report", "no-exit-report", loaded.Values.Merge.ExitReport)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options.Output, _ = resolveStringOption(c, "output", loaded.Values.Merge.Output)
	options.Overwrite, _, err = resolveBoolOptionWithNegative(c, "overwrite", "no-overwrite", loaded.Values.Merge.Overwrite)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options.Progress, _, err = resolveBoolOptionWithNegative(c, "progress", "no-progress", loaded.Values.Merge.Progress)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options.Unsafe, _, err = resolveBoolOptionWithNegative(c, "unsafe", "safe", loaded.Values.Merge.Unsafe)
	if err != nil {
		return mergeCommandOptions{}, err
	}
	options.Verbosity, _ = resolveIntOption(c, "verbosity", loaded.Values.Merge.Verbosity)
	options.Inputs = append([]string(nil), []string(c.Args())...)
	return options, nil
}
