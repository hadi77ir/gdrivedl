// Package main (goodls.go) :
// These methods are for downloading shared files from Google Drive.
package gdrivedl

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/urfave/cli"
	"golang.org/x/net/html"
	"golang.org/x/term"
)

const (
	appname                    = "gdrivedl"
	envval                     = "GDRIVEDL_APIKEY"
	anyurl                     = "https://drive.google.com/uc?export=download"
	docutl                     = "https://docs.google.com/"
	googleLoginRequiredMessage = "Google login/sign-in is required for this link. gdrivedl only supports public links that do not require interactive Google sign-in."
	appContextMetadataKey      = "gdrivedl-context"
)

// chunks : For io.Reader
type chunks struct {
	io.Reader
	cChunk int64
	Size   int64
}

// para : Structure for each parameter
type para struct {
	APIKey                string
	Client                *http.Client
	CompletionReport      bool
	ContentType           string
	Disp                  bool
	DlFolder              bool
	DownloadBytes         int64
	DryRun                bool
	EnableProgress        bool
	Ext                   string
	ExitReport            bool
	Filename              string
	ID                    string
	InputtedMimeType      []string
	JSONOutput            bool
	Kind                  string
	MaxConcurrency        int
	Notcreatetopdirectory bool
	OverWrite             bool
	Resumabledownload     string
	Runtime               *downloadRuntime
	Scan                  bool
	SearchID              string
	ShowFileInf           bool
	Size                  int64
	Skip                  bool
	SkipError             bool
	Task                  *downloadTask
	TransportConfig       transportConfig
	URL                   string
	Context               context.Context
	WorkDir               string
	URLForLargeFile       string
}

func (p *para) clone() *para {
	if p == nil {
		return nil
	}
	clone := *p
	clone.Client = nil
	clone.Task = nil
	if p.InputtedMimeType != nil {
		clone.InputtedMimeType = append([]string(nil), p.InputtedMimeType...)
	}
	return &clone
}

func (p *para) printf(format string, args ...interface{}) {
	if p != nil && p.Runtime != nil {
		p.Runtime.printf(format, args...)
		return
	}
	fmt.Printf(format, args...)
}

func (p *para) forceJSONLogs() bool {
	return p != nil && p.Runtime != nil && p.Runtime.forceJSONLogs()
}

func (p *para) statusf(format string, args ...interface{}) {
	if p == nil {
		return
	}
	if p.Runtime != nil {
		message := fmt.Sprintf(format, args...)
		p.Runtime.log("status", "[status] "+message+"\n", map[string]any{"message": message})
		return
	}
	p.printf("[status] "+format+"\n", args...)
}

func (p *para) reportEvent(name string, fields map[string]any) {
	if p == nil || p.Runtime == nil {
		return
	}
	p.Runtime.report(name, fields)
}

func (p *para) traceRequest(req *http.Request) *http.Request {
	return withRequestTrace(req, p.Runtime, p.Task)
}

// getURLFromHTML : Get the download URL from HTML. This is used from January 2024.
func (p *para) getURLFromHTML(res *http.Response) error {
	doc, err := html.Parse(res.Body)
	if err != nil {
		return err
	}
	form := findHTMLNode(doc, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "form" && htmlAttr(node, "id") == "download-form"
	})
	if form == nil {
		return fmt.Errorf("Specification of the endpoint for downloading the file might have been changed.")
	}
	url := htmlAttr(form, "action")
	req, err := http.NewRequestWithContext(p.requestContext(), "GET", url, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	walkHTML(form, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "type") == "hidden" {
			name := htmlAttr(node, "name")
			if name != "" {
				q.Add(name, htmlAttr(node, "value"))
			}
		}
	})
	req.URL.RawQuery = q.Encode()
	p.URLForLargeFile = req.URL.String()
	return nil
}

func htmlAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func findHTMLNode(node *html.Node, predicate func(*html.Node) bool) *html.Node {
	if predicate(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLNode(child, predicate); found != nil {
			return found
		}
	}
	return nil
}

func walkHTML(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func (p *para) detectGoogleLoginRequirement(res *http.Response) error {
	if p == nil || res == nil {
		return nil
	}
	if responseRequiresGoogleLogin(res) {
		return errors.New(googleLoginRequiredMessage)
	}
	return nil
}

func responseRequiresGoogleLogin(res *http.Response) bool {
	if res == nil {
		return false
	}
	if res.Request != nil && googleLoginURL(res.Request.URL) {
		return true
	}
	if len(res.Header["Content-Disposition"]) > 0 {
		return false
	}
	contentType := strings.ToLower(res.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "text/html") {
		return false
	}
	return bodyRequiresGoogleLogin(res)
}

func bodyRequiresGoogleLogin(res *http.Response) bool {
	if res == nil || res.Body == nil {
		return false
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return false
	}
	res.Body = io.NopCloser(bytes.NewReader(body))
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "accounts.google.com/servicelogin") || strings.Contains(lower, "/servicelogin") {
		return true
	}
	hasSignInText := strings.Contains(lower, "<title>sign in") || strings.Contains(lower, ">sign in<") || strings.Contains(lower, "sign in - google accounts")
	hasGoogleAccountMarker := strings.Contains(lower, "identifierid") || strings.Contains(lower, "google accounts") || strings.Contains(lower, "accounts.google.com")
	return hasSignInText && hasGoogleAccountMarker
}

func googleLoginURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	if host == "accounts.google.com" {
		return true
	}
	return strings.Contains(path, "servicelogin")
}

// Read : For io.Reader
func (c *chunks) Read(dat []byte) (int, error) {
	n, err := c.Reader.Read(dat)
	c.cChunk += int64(n)
	if err == nil {
		if c.Size > 0 {
			fmt.Printf("\rDownloading (bytes)... %d / %d", c.cChunk, c.Size)
		} else {
			fmt.Printf("\rDownloading (bytes)... %d", c.cChunk)
		}
	}
	return n, err
}

// saveFile : Save retrieved data as a file.
func (p *para) saveFile(res *http.Response) (err error) {
	if p.DryRun {
		return p.completeDryRun(res)
	}
	defer res.Body.Close()
	p.ContentType = res.Header.Get("Content-Type")
	if err = p.getFilename(res); err != nil {
		return err
	}
	if p.Task != nil {
		p.Task.SetName(p.Filename)
		switch {
		case p.Size > 0:
			p.Task.SetTotal(p.Size)
		case res.ContentLength > 0:
			p.Task.SetTotal(res.ContentLength)
		}
		p.Task.MarkStarted()
		p.Task.SetState("downloading")
		res.Body = &trackedReadCloser{ReadCloser: res.Body, task: p.Task}
	}
	var file *os.File
	if p.DownloadBytes == -1 {
		file, err = os.Create(filepath.Join(p.WorkDir, p.Filename))
	} else {
		file, err = os.OpenFile(filepath.Join(p.WorkDir, p.Filename), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	}
	if err != nil {
		return err
	}
	defer file.Close()
	showLegacyProgress := !p.JSONOutput && !p.Disp && !p.EnableProgress && p.MaxConcurrency <= 1
	bodyReader := io.Reader(res.Body)
	if showLegacyProgress {
		if p.APIKey != "" {
			bodyReader = &chunks{Reader: bodyReader, Size: p.Size}
		} else {
			bodyReader = &chunks{Reader: bodyReader}
		}
	}
	if p.Disp {
		_, err = io.Copy(file, bodyReader)
	} else {
		_, err = io.Copy(file, bodyReader)
	}
	if err != nil {
		if isCancellationError(err) {
			_ = file.Sync()
		}
		return err
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	if showLegacyProgress {
		p.printf("\n")
	}
	if p.Task != nil {
		p.Task.MarkCompleted()
	}
	p.reportEvent("download_complete", map[string]any{
		"file":      p.Filename,
		"kind":      p.Kind,
		"mime_type": p.ContentType,
		"file_size": fileInfo.Size(),
	})
	if p.TransportConfig.Verbosity >= 2 || p.forceJSONLogs() {
		p.printf("[http] %s | response body complete bytes=%d\n", firstNonEmpty(p.Filename, p.URL), fileInfo.Size())
	}
	if p.CompletionReport {
		p.printf("Completed: %s | Type: %s | MimeType: %s | FileSize: %d\n", p.Filename, p.Kind, p.ContentType, fileInfo.Size())
	}
	return nil
}

func (p *para) completeDryRun(res *http.Response) error {
	if res == nil {
		return fmt.Errorf("dry run requires a response")
	}
	defer res.Body.Close()
	p.ContentType = res.Header.Get("Content-Type")
	if p.Filename == "" && len(res.Header["Content-Disposition"]) > 0 {
		if err := p.getFilename(res); err != nil {
			return err
		}
	}
	if p.Task != nil {
		if p.Filename != "" {
			p.Task.SetName(p.Filename)
		}
		switch {
		case p.Size > 0:
			p.Task.SetTotal(p.Size)
			p.Task.SetDownloaded(p.Size)
		case res.ContentLength > 0:
			p.Task.SetTotal(res.ContentLength)
			p.Task.SetDownloaded(res.ContentLength)
		}
		p.Task.MarkStarted()
		p.Task.SetDetail("dry run")
		p.Task.MarkCompleted()
		p.Task.SetState("dry run complete")
	}
	p.reportEvent("dry_run_complete", map[string]any{
		"file":        firstNonEmpty(p.Filename, p.URL, "(unknown)"),
		"kind":        p.Kind,
		"mime_type":   p.ContentType,
		"http_status": res.Status,
	})
	if p.TransportConfig.Verbosity >= 2 || p.forceJSONLogs() {
		p.printf("[http] %s | dry run complete status=%s\n", firstNonEmpty(p.Filename, p.URL, "(unknown)"), res.Status)
	}
	if p.CompletionReport {
		p.printf("Dry run: %s | Type: %s | MimeType: %s | HTTPStatus: %s\n", firstNonEmpty(p.Filename, p.URL, "(unknown)"), p.Kind, p.ContentType, res.Status)
	}
	return nil
}

// getFilename : Retrieve filename from header.
func (p *para) getFilename(s *http.Response) error {
	if len(s.Header["Content-Disposition"]) > 0 {
		_, para, err := mime.ParseMediaType(s.Header["Content-Disposition"][0])
		if err != nil {
			return err
		}
		if p.Filename == "" {
			p.Filename = para["filename"]
		}
	} else {
		body, _ := io.ReadAll(s.Body)
		rFilename := regexp.MustCompile(`<span class="uc-name-size"><a[\w\s\S]+?>([\w\s\S]+?)<\/a>`)
		matches := rFilename.FindAllStringSubmatch(string(body), -1)
		if len(matches) == 0 {
			return fmt.Errorf("file ID [ %s ] cannot be downloaded", p.ID)
		}
		p.Filename = matches[0][1]
	}
	return nil
}

// downloadLargeFile : When a large size of file is downloaded, this method is used.
func (p *para) downloadLargeFile() error {
	if p.DryRun {
		p.printf("Now testing download request.\n")
	} else {
		p.printf("Now downloading.\n")
	}
	if p.APIKey != "" {
		dlfile, err := p.getFileInfFromP()
		if err != nil {
			return err
		}
		p.Size = dlfile.Size
	}
	res, err := p.fetch(p.URLForLargeFile)
	if err != nil {
		return err
	}
	if err := p.detectGoogleLoginRequirement(res); err != nil {
		res.Body.Close()
		return err
	}
	if res.StatusCode != 200 && p.Kind != "file" {
		return fmt.Errorf("error: This error occurs when it downloads a large file of Google Docs.\nMessage: %+v", res)
	}
	return p.saveFile(res)
}

// fetch : Fetch data from Google Drive
func (p *para) fetch(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(p.requestContext(), "get", url, nil)
	if err != nil {
		return nil, err
	}
	req = p.traceRequest(req)
	res, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// checkURL : Parse inputted URL.
func (p *para) checkURL(s string) error {
	var err error
	r := regexp.MustCompile(`google\.com\/(\w.+)\/d\/(\w.+)\/`)
	r2 := regexp.MustCompile(`drive.google.com\/uc\?(export\=\w+|id\=([\w\S]+))&(export\=\w+|id\=([\w\S]+))`)
	if r.MatchString(s) {
		res := r.FindAllStringSubmatch(s, -1)
		p.Kind = res[0][1]
		p.ID = res[0][2]
		if p.Kind == "file" {
			p.URL = anyurl + "&id=" + p.ID
		} else {
			if p.Ext == "" {
				p.Ext = "pdf"
			} else if p.Ext == "ms" {
				switch p.Kind {
				case "spreadsheets":
					p.Ext = "xlsx"
				case "document":
					p.Ext = "docx"
				case "presentation":
					p.Ext = "pptx"
				}
			}
			if p.Kind == "presentation" {
				p.URL = docutl + p.Kind + "/d/" + p.ID + "/export/" + p.Ext
			} else {
				p.URL = docutl + p.Kind + "/d/" + p.ID + "/export?format=" + p.Ext
			}
		}

		if p.APIKey != "" && p.Kind == "file" && p.Resumabledownload == "" {
			if p.DryRun {
				p.printf("Now testing with API key.\n")
			} else {
				p.printf("Now downloading with API key.\n")
			}
			p.URL = "https://www.googleapis.com/drive/v3/files/" + p.ID + "?alt=media&supportsAllDrives=true&key=" + p.APIKey
			dlfile, err := p.getFileInfFromP()
			if err != nil {
				return err
			}
			p.Filename = dlfile.Name
			p.Size = dlfile.Size
		}

		if p.APIKey != "" && p.ShowFileInf {
			if err := p.showFileInf(); err != nil {
				return err
			}
			return nil
		}
	} else if r2.MatchString(s) {
		u, err := url.Parse(s)
		if err != nil {
			return err
		}
		q := u.Query()
		p.Kind = "file"
		p.ID = q["id"][0]
		p.URL = anyurl + "&id=" + p.ID
		if p.APIKey != "" && p.ShowFileInf {
			if err := p.showFileInf(); err != nil {
				return err
			}
			return nil
		}
	} else {
		folder := regexp.MustCompile(`google\.com\/drive\/folders\/([a-zA-Z0-9-_]+)`)
		if folder.MatchString(s) {
			p.DlFolder = true
			res := folder.FindAllStringSubmatch(s, -1)
			p.SearchID = res[0][1]
			err = p.getFilesFromFolder()
			if err != nil {
				return err
			}
		} else {
			return errors.New("URL is wrong")
		}
	}
	return nil
}

func (p *para) shouldUseResumableFlow() bool {
	return p.APIKey != "" && p.Resumabledownload != "" && p.Kind == "file"
}

func (p *para) downloadResolvedURL() error {
	if p.Task != nil {
		p.Task.SetState("preparing request")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	p.Client, err = p.newHTTPClient(jar)
	if err != nil {
		return err
	}
	res, err := p.fetch(p.URL)
	if err != nil {
		return err
	}
	if err := p.detectGoogleLoginRequirement(res); err != nil {
		res.Body.Close()
		return err
	}
	if res.StatusCode == 200 {
		_, chk := res.Header["Content-Disposition"]
		if chk {
			return p.saveFile(res)
		}
		if err := p.getURLFromHTML(res); err != nil {
			return err
		}
		if len(p.URLForLargeFile) == 0 && p.Kind == "file" {
			return fmt.Errorf("file ID [ %s ] is not shared, while the file is existing", p.ID)
		}
		if len(p.URLForLargeFile) == 0 && p.Kind != "file" {
			return p.saveFile(res)
		}
		return p.downloadLargeFile()
	}
	return fmt.Errorf("file ID [ %s ] cannot be downloaded as [ %s ]", p.ID, p.Ext)
}

func (p *para) downloadWithResumableFallback() error {
	if p.Task != nil {
		p.Task.SetState("checking local file")
	}
	fileExists, completed, err := p.precheckResumableDownload()
	if err != nil {
		return err
	}
	if fileExists || completed {
		return p.resumableDownload()
	}
	shared := p.clone()
	shared.APIKey = ""
	shared.Resumabledownload = ""
	shared.Client = nil
	shared.Task = p.Task
	if err := shared.downloadResolvedURL(); err == nil {
		p.Filename = shared.Filename
		if shared.Size > 0 {
			p.Size = shared.Size
		}
		return nil
	}
	return p.resumableDownload()
}

// download : Main method of download.
func (p *para) download(url string) (err error) {
	err = p.checkURL(url)
	if err != nil {
		return err
	}
	if p.DlFolder {
		return nil
	}
	if p.APIKey != "" && p.ShowFileInf {
		return nil
	} else if p.APIKey == "" && p.ShowFileInf {
		return errors.New("when you want to use the option '--file-info', please use API key")
	}
	if p.Runtime != nil && p.Task == nil {
		p.Task = p.Runtime.newTask(firstNonEmpty(p.Filename, url), url)
		if p.Size > 0 {
			p.Task.SetTotal(p.Size)
		}
		defer func() {
			if err != nil {
				finishTaskWithError(p.Task, err)
			}
		}()
	}
	if p.shouldUseResumableFlow() {
		return p.downloadWithResumableFallback()
	}
	return p.downloadResolvedURL()
}

func commandContext(c *cli.Context) context.Context {
	if c != nil && c.App != nil && c.App.Metadata != nil {
		if ctx, ok := c.App.Metadata[appContextMetadataKey].(context.Context); ok && ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

func handleRootCommand(c *cli.Context) error {
	cli.ShowAppHelp(c)
	return nil
}

// handleGetCommand initializes and runs the download command.
func handleGetCommand(c *cli.Context) error {
	cfg, err := parseConfig(c, os.Getenv, filepath.Abs)
	if err != nil {
		return err
	}
	if cfg.URL != "" && cfg.URLList != "" {
		return fmt.Errorf("please use either '--url' or '--url-list'")
	}
	if cfg.URLList != "" && cfg.Filename != "" {
		return fmt.Errorf("'--filename' cannot be used with '--url-list'")
	}
	p := cfg.toPara()
	p.Context = commandContext(c)
	if cfg.EnableProgress || cfg.ExitReport || cfg.MaxConcurrency > 1 || cfg.JSONOutput {
		p.Runtime = newObservedDownloadRuntime(cfg.EnableProgress, cfg.ExitReport, cfg.JSONOutput, nil, nil)
		p.Runtime.start()
		defer p.Runtime.finish()
	}
	if cfg.URLList != "" {
		urls, err := readURLList(cfg.URLList, os.Stdin)
		if err != nil {
			return err
		}
		return downloadURLList(p, urls)
	}
	if cfg.URL != "" {
		err = p.download(cfg.URL)
		if err != nil {
			return err
		}
		return nil
	}
	if term.IsTerminal(int(syscall.Stdin)) {
		cli.ShowCommandHelp(c, "get")
		return nil
	} else {
		urls, err := readURLs(os.Stdin)
		if err != nil {
			return err
		}
		return downloadURLList(p, urls)
	}
	return nil
}

func handleScanCommand(c *cli.Context) error {
	options, err := parseScanCommandOptionsWithEnv(c, defaultCLIParseEnvironment())
	if err != nil {
		return err
	}
	if options.ScanDomainList == "-" && options.ScanIPList == "-" {
		return fmt.Errorf("--scan-domain-list - cannot share standard input with --scan-ip-list -")
	}
	transport, err := buildScanTransportConfigFromOptions(options.transportOptionFlags)
	if err != nil {
		return err
	}
	resolveToAddrs, err := parseResolveToList(options.ResolveTo)
	if err != nil {
		return err
	}
	frontingTargets, err := parseHostnameList(options.FrontingTarget, "--fronting-target")
	if err != nil {
		return err
	}
	frontingSNIs, err := parseHostnameList(options.FrontingSNI, "--fronting-sni")
	if err != nil {
		return err
	}
	extraScanDomains := []string(nil)
	extraScanIPs := []string(nil)
	if options.ScanDomainList != "" {
		extraScanDomains, err = readHostnameList(options.ScanDomainList, os.Stdin)
		if err != nil {
			return err
		}
	}
	if options.ScanIPList != "" {
		extraScanIPs, err = readIPSpecList(options.ScanIPList, os.Stdin)
		if err != nil {
			return err
		}
	}
	var runtime *downloadRuntime
	if options.JSONOutput || transport.Verbosity > 0 {
		runtime = newObservedDownloadRuntime(false, false, options.JSONOutput, nil, nil)
	}
	report, scanErr := runConnectivityScan(commandContext(c), transport, connectivityScanOptions{
		Mode:            options.ScanMode,
		ExtraDomains:    extraScanDomains,
		ExtraIPSpecs:    extraScanIPs,
		FrontingSNIs:    frontingSNIs,
		FrontingTargets: frontingTargets,
		ResolveToAddrs:  resolveToAddrs,
	}, runtime, scanDependencies{})
	if options.JSONOutput {
		runtime.report("scan", report.fields())
	} else {
		report.print(os.Stdout)
	}
	if scanErr == nil && strings.TrimSpace(options.SavePath) != "" {
		if err := saveScanReportYAML(options.SavePath, report); err != nil {
			return err
		}
	}
	return scanErr
}

func handleMergeCommand(c *cli.Context) error {
	options, err := parseMergeCommandOptionsWithEnv(c, defaultCLIParseEnvironment())
	if err != nil {
		return err
	}
	return Merge(commandContext(c), MergeRequest{
		DeleteChunks:         options.DeleteChunks,
		Inputs:               options.Inputs,
		Output:               options.Output,
		Overwrite:            options.Overwrite,
		ExitReport:           options.ExitReport,
		JSONOutput:           options.JSONOutput,
		ShowTerminalProgress: options.Progress,
		Unsafe:               options.Unsafe,
		Verbosity:            options.Verbosity,
	})
}

func handleTestConnectivityCommand(c *cli.Context) error {
	options, err := parseTestCommandOptionsWithEnv(c, defaultCLIParseEnvironment())
	if err != nil {
		return err
	}
	transport, err := buildTransportConfigFromOptions(options.transportOptionFlags)
	if err != nil {
		return err
	}
	runtime := (*downloadRuntime)(nil)
	if options.JSONOutput {
		runtime = newObservedDownloadRuntime(false, false, true, nil, nil)
	}
	report, err := testConnectivityWithTransport(commandContext(c), transport, runtime, scanProbeURL)
	if !options.JSONOutput {
		report.print(os.Stdout)
	}
	return err
}

type urlDownloadJob struct {
	URL  string
	Task *downloadTask
}

func buildURLDownloadJobs(p *para, urls []string) []urlDownloadJob {
	jobs := make([]urlDownloadJob, 0, len(urls))
	for _, rawURL := range urls {
		job := urlDownloadJob{URL: rawURL}
		if p != nil && p.Runtime != nil {
			job.Task = p.Runtime.newTask(rawURL, rawURL)
		}
		jobs = append(jobs, job)
	}
	return jobs
}

func downloadURLList(p *para, urls []string) error {
	if len(urls) == 0 {
		return fmt.Errorf("no URL data. Please check help\n\n $ %s --help", appname)
	}
	jobs := buildURLDownloadJobs(p, urls)
	workers := p.MaxConcurrency
	if workers < 1 {
		workers = 1
	}
	jobCh := make(chan urlDownloadJob)
	ctx := p.requestContext()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobCh:
					if !ok {
						return
					}
					jobPara := p.clone()
					jobPara.Filename = ""
					jobPara.Task = job.Task
					if err := jobPara.download(job.URL); err != nil {
						jobPara.printf("## Skipped: Error: %v\n", err)
					}
				}
			}
		}()
	}
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			close(jobCh)
			wg.Wait()
			return ctx.Err()
		case jobCh <- job:
		}
	}
	close(jobCh)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func readURLList(source string, stdin io.Reader) ([]string, error) {
	if source == "-" {
		return readURLs(stdin)
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readURLs(file)
}

func readURLs(reader io.Reader) ([]string, error) {
	var urls []string
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "", line == "end", strings.HasPrefix(line, "#"):
			continue
		default:
			urls = append(urls, line)
		}
	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	return urls, nil
}

// createHelp : Create help document.
func createHelp() *cli.App {
	a := cli.NewApp()
	a.Name = appname
	a.Authors = []cli.Author{
		{Name: "hadi77ir", Email: "https://github.com/hadi77ir/gdrivedl"},
	}
	a.Usage = "CLI and Go library for public Google Drive downloads and transport probing."
	a.UsageText = "gdrivedl <command> [command options] [arguments...]"
	a.Description = "gdrivedl downloads public Google Drive files and folders, probes direct and fronted network routes, and merges split chunk folders. Run 'gdrivedl help <command>' for command-specific usage. Each subcommand accepts '--config' and loads defaults from '$XDG_CONFIG_DIR/gdrivedl.yml', then '$XDG_CONFIG_HOME/gdrivedl.yml', when those files exist."
	a.Version = Version
	a.Commands = []cli.Command{
		{
			Name:        "get",
			Usage:       "Download public Google Drive files, folders, and URL lists.",
			UsageText:   "gdrivedl get --url <drive-url> [options]\n   gdrivedl get --url-list <file|-> [options]",
			Description: "Download shared Google Drive files or folders without OAuth, or process many URLs from a file or standard input. Use '--config' to load default command values from YAML.",
			Action:      handleGetCommand,
			Flags:       getCommandCLIFlags(),
		},
		{
			Name:        "scan",
			Usage:       "Probe viable direct and fronted transport routes.",
			UsageText:   "gdrivedl scan [options]",
			Description: "Probe https://gstatic.com/generate_204 in selectable phases. 'full' runs IP discovery first and then fronting-domain/SNI probing, 'only-ip' scans accessible IPs, and 'only-domains' validates fronting targets and SNIs against the provided --resolve-to IP list. The report prints reusable --fronting-target, --fronting-sni, --resolve-to, and --utls-profile values, and '--save' can merge those values into a YAML config file. Use '--config' to load default scan settings from YAML.",
			Action:      handleScanCommand,
			Flags:       scanCommandCLIFlags(),
		},
		{
			Name:        "test",
			Aliases:     []string{"probe"},
			Usage:       "Send one transport probe to https://gstatic.com/generate_204.",
			UsageText:   "gdrivedl test [options]",
			Description: "Validate a candidate proxy, resolve-to IP, fronting target, HTTP version preference, and uTLS profile without starting a download. Use '--config' to load default transport settings from YAML.",
			Action:      handleTestConnectivityCommand,
			Flags:       testCommandCLIFlags(),
		},
		{
			Name:        "merge",
			Aliases:     []string{"combine"},
			Usage:       "Merge chunk files into one file with safe mode enabled by default.",
			UsageText:   "gdrivedl merge --output <file> [merge options] [input paths...]",
			Description: "Combine chunk files from a single folder or from multiple split_nnnn folders into one final output. Safe mode writes to a temporary file first and supports cancellation. Use '--config' to load default merge settings from YAML.",
			Action:      handleMergeCommand,
			Flags:       mergeCLIFlags(),
		},
	}
	return a
}

func RunCLI(args []string) error {
	a := createHelp()
	a.Action = handleRootCommand
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if a.Metadata == nil {
		a.Metadata = map[string]interface{}{}
	}
	a.Metadata[appContextMetadataKey] = ctx
	return a.Run(args)
}
