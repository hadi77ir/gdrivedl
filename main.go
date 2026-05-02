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
	Kind                  string
	MaxConcurrency        int
	Notcreatetopdirectory bool
	OverWrite             bool
	Resumabledownload     string
	Runtime               *downloadRuntime
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

func (p *para) statusf(format string, args ...interface{}) {
	if p == nil {
		return
	}
	p.printf("[status] "+format+"\n", args...)
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
	showLegacyProgress := !p.Disp && !p.EnableProgress && p.MaxConcurrency <= 1
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
	if p.TransportConfig.Verbosity >= 2 {
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
	if p.TransportConfig.Verbosity >= 2 {
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
		return errors.New("when you want to use the option '--fileinf', please use API key")
	}
	if p.Runtime != nil && p.Task == nil {
		p.Task = p.Runtime.newTask(firstNonEmpty(p.Filename, url), url)
		if p.Size > 0 {
			p.Task.SetTotal(p.Size)
		}
		defer func() {
			if err != nil {
				p.Task.MarkFailed(err)
			}
		}()
	}
	if p.shouldUseResumableFlow() {
		return p.downloadWithResumableFallback()
	}
	return p.downloadResolvedURL()
}

// handler : Initialize of "para".
func handler(c *cli.Context) error {
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
	if cfg.EnableProgress || cfg.ExitReport || cfg.MaxConcurrency > 1 {
		p.Runtime = newDownloadRuntime(cfg.EnableProgress, cfg.ExitReport)
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
		if cfg.URL == "" {
			createHelp().Run(os.Args)
			return nil
		}
	} else {
		urls, err := readURLs(os.Stdin)
		if err != nil {
			return err
		}
		return downloadURLList(p, urls)
	}
	return nil
}

func downloadURLList(p *para, urls []string) error {
	if len(urls) == 0 {
		return fmt.Errorf("no URL data. Please check help\n\n $ %s --help", appname)
	}
	workers := p.MaxConcurrency
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan string)
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
				case rawURL, ok := <-jobs:
					if !ok {
						return
					}
					jobPara := p.clone()
					jobPara.Filename = ""
					if err := jobPara.download(rawURL); err != nil {
						jobPara.printf("## Skipped: Error: %v\n", err)
					}
				}
			}
		}()
	}
	for _, rawURL := range urls {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- rawURL:
		}
	}
	close(jobs)
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
		{Name: "hadi77ir [ https://github.com/hadi77ir/gdrivedl ] "},
	}
	a.UsageText = "Download shared files on Google Drive."
	a.Version = Version
	a.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "url, u",
			Usage: "URL of shared file on Google Drive. Required unless --url-list is used.",
		},
		&cli.StringFlag{
			Name:  "url-list",
			Usage: "Path to a newline-delimited URL list. Use '-' to read the list from standard input.",
		},
		&cli.StringFlag{
			Name:  "extension, e",
			Usage: "Extension of output file. This is for only Google Docs (Spreadsheet, Document, Presentation).",
			Value: "pdf",
		},
		&cli.StringFlag{
			Name:  "filename, f",
			Usage: "Filename of file which is output. When this was not used, the original filename on Google Drive is used.",
		},
		&cli.StringSliceFlag{
			Name:  "mimetype, m",
			Usage: "mimeType (You can retrieve only files with the specific mimeType, when files are downloaded from a folder.) ex. '-m \"mimeType1,mimeType2\"'",
		},
		&cli.StringFlag{
			Name:  "resumabledownload, r",
			Usage: "File is downloaded as the resumable download. For example, when '-r 1m' is used, the size of 1 MB is downloaded and create new file or append the existing file. API key is required.",
		},
		&cli.BoolFlag{
			Name:  "NoProgress, np",
			Usage: "When this option is used, the progression is not shown.",
		},
		&cli.BoolFlag{
			Name:  "progress",
			Usage: "Show aggregate download progress with current file, total progress, speed, and ETA.",
		},
		&cli.IntFlag{
			Name:  "concurrency",
			Usage: "Maximum number of concurrent downloads for URL lists and folder downloads.",
			Value: 1,
		},
		&cli.BoolFlag{
			Name:  "exit-report",
			Usage: "Print a final per-file download report on exit.",
		},
		&cli.BoolFlag{
			Name:  "completion-report",
			Usage: "Print a per-file completion report line after each successful download.",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Send download requests for testing without saving files or creating directories.",
		},
		&cli.BoolFlag{
			Name:  "overwrite, o",
			Usage: "When filename of downloading file is existing in directory at local PC, overwrite it. At default, it is not overwritten.",
		},
		&cli.BoolFlag{
			Name:  "skip, s",
			Usage: "When filename of downloading file is existing in directory at local PC, skip it. At default, it is not overwritten.",
		},
		&cli.BoolFlag{
			Name:  "fileinf, i",
			Usage: "Retrieve file information. API key is required for individual file metadata; public folder links can be listed without an API key.",
		},
		&cli.StringFlag{
			Name:  "apikey, key",
			Usage: "Optional Google API key used for resumable downloads, file metadata, and Drive API-based folder access.",
		},
		&cli.StringFlag{
			Name:  "directory, d",
			Usage: "Directory for saving downloaded files. When this is not used, the files are saved to the current working directory.",
		},
		&cli.BoolFlag{
			Name:  "notcreatetopdirectory, ntd",
			Usage: "When this option is NOT used (default situation), when a folder including subfolders is downloaded, the top folder which is downloaded is created as the top directory under the working directory. When this option is used, the top directory is not created and all files and subfolders under the top folder are downloaded under the working directory.",
		},
		&cli.BoolFlag{
			Name:  "skiperror, se",
			Usage: "When the files are downloaded from the folder, if an error occurs, the error is skipped by this option.",
		},
		&cli.StringFlag{
			Name:  "proxy",
			Usage: "Upstream proxy URL. Supported schemes are http:// and socks5://. Example: http://127.0.0.1:2089.",
		},
		&cli.StringFlag{
			Name:  "timeout",
			Usage: "HTTP client timeout. Accepts Go duration strings like 30s, 2m, or plain seconds like 60.",
		},
		&cli.StringFlag{
			Name:  "request-delay",
			Usage: "Minimum delay between HTTP requests. Accepts Go duration strings like 500ms, 2s, or plain seconds like 1.",
		},
		&cli.IntFlag{
			Name:  "verbosity",
			Usage: "HTTP logging verbosity level. 0 disables stage logs, 1 enables stage and status logs, 2 adds detailed connection logs.",
			Value: 0,
		},
		&cli.BoolFlag{
			Name:  "dump-request",
			Usage: "Dump outgoing HTTP requests before they are sent.",
		},
		&cli.BoolFlag{
			Name:  "dump-response",
			Usage: "Dump received HTTP response headers after they are received.",
		},
		&cli.StringFlag{
			Name:  "resolve-to",
			Usage: "Override the network dial IP for requests while preserving the original request port and logical host. Use an IP address.",
		},
		&cli.StringFlag{
			Name:  "utls-profile",
			Usage: "uTLS ClientHello profile for HTTPS requests. Supported values include chrome_auto, firefox_auto, safari_auto, ios_auto, edge_auto, 360_auto, qq_auto, randomized, randomized_alpn, and randomized_no_alpn.",
		},
		&cli.BoolFlag{
			Name:  "prefer-http2",
			Usage: "Prefer HTTP/2 over HTTP/1.1 for HTTPS requests when the server supports it.",
		},
		&cli.BoolFlag{
			Name:  "force-http1",
			Usage: "Force HTTP/1.1 for HTTPS requests and disable HTTP/2 negotiation.",
		},
		&cli.BoolFlag{
			Name:  "share-http2-connection",
			Usage: "Reuse a negotiated HTTP/2 TLS connection for multiple requests to the same target. Implies HTTP/2 preference and cannot be combined with --force-http1.",
		},
		&cli.BoolFlag{
			Name:  "fronting-enable",
			Usage: "Enable HTTP domain fronting in the shared transport for all requests.",
		},
		&cli.StringFlag{
			Name:  "fronting-sni",
			Usage: "Optional TLS SNI override for fronted requests. Defaults to the fronting target hostname. Requires --fronting-enable.",
		},
		&cli.StringFlag{
			Name:  "fronting-target",
			Usage: "Fronting target hostname used for network dial. The original request port is preserved. Requires --fronting-enable.",
		},
	}
	return a
}

func RunCLI(args []string) error {
	a := createHelp()
	a.Action = handler
	return a.Run(args)
}
