// Package main (getfilesfromfolder.go) :
// These methods are for downloading all files from a shared folder of Google Drive.
package gdrivedl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	drive "google.golang.org/api/drive/v3"
)

const (
	driveAPI                   = "https://www.googleapis.com/drive/v3/files"
	publicDriveFrontendAPI     = "https://drivefrontend-pa.clients6.google.com"
	publicDriveFrontendAPIKey  = "AIzaSyC1qbk75NzWBvSaDh6KnsjjA9pIrP4lYIE"
	publicDriveFoldersPageBase = "https://drive.google.com/drive/folders/"
	publicDriveFieldMaskGet    = "responses(status(code,message,details),item(parent,modified_date_millis,modified_by_me_date_millis,last_viewed_by_me_date_millis,file_size,owner(id,focus_user_id,is_me,type),shortcut_details(target_id,target_mime_type,target_lookup_status,target_item,can_request_access_to_target),last_modifying_user(id,focus_user_id,is_me,type),has_thumbnail,thumbnail_version,title,mime_type,id,resource_key,shared,shared_with_me_date_millis,capabilities(can_copy_non_authoritative,can_download_non_authoritative,can_copy,can_download,can_edit,can_add_children,can_delete,can_remove_children,can_share,can_trash,can_rename,can_read_team_drive,can_move_team_drive_item,can_move_item_into_team_drive,can_untrash,can_modify_content_restriction,can_move_item_within_team_drive,can_move_item_out_of_team_drive,can_delete_children,can_trash_children,can_request_approval,can_read_category_metadata,can_edit_category_metadata,can_add_my_drive_parent,can_remove_my_drive_parent,can_read,can_move_item_within_drive,can_move_children_within_drive,can_add_folder_from_another_drive,can_change_security_update_enabled,can_create_decrypted_copy,can_create_encrypted_copy,can_add_encrypted_children,can_block_owner,can_report_spam_or_abuse,can_copy_encrypted_file,can_report_not_spam,can_initiate_esignature,can_discover_by_search,can_list_children),user_role,explicitly_trashed,quota_bytes_used,starred,file_extension,sharing_user(id,focus_user_id,is_me,type),spaces,trashed,restricted,version,viewed,team_drive_id,has_own_permissions,create_date_millis,trashing_user(id,focus_user_id,is_me,type),trashed_date_millis,has_visitor_permissions,alternate_link,workspace_id,content_restrictions(read_only),approval_version,approval_summaries,customer_id,ancestor_has_own_permissions,abuse_is_appealable,abuse_notice_reason,spam_metadata(marked_as_spam_date_millis,in_spam_view,is_spam,is_inherited_spam),access_requests_count,has_incoming_approval,inheritance_broken,gmail_message_storage_id,applied_labels,has_catch_me_up_content,workflow_creation_id,vids_import_compatibility_info,workbook_details,subscribed,folder_color,has_child_folder,creator_app_id,primary_sync_parent,flagged_for_abuse,folder_features,source_app_id,recency_date_millis,recency_date_reason,action_item,primary_domain_name,organization_display_name))"
	publicDriveFieldMaskList   = "items(parent,modified_date_millis,modified_by_me_date_millis,last_viewed_by_me_date_millis,file_size,owner(id,focus_user_id,is_me,type),shortcut_details(target_id,target_mime_type,target_lookup_status,target_item),last_modifying_user(id,focus_user_id,is_me,type),has_thumbnail,thumbnail_version,title,mime_type,id,resource_key,shared,shared_with_me_date_millis,capabilities(can_copy_non_authoritative,can_download_non_authoritative,can_copy,can_download,can_edit,can_add_children,can_delete,can_remove_children,can_share,can_trash,can_rename,can_read_team_drive,can_move_team_drive_item,can_delete_children),user_role,explicitly_trashed,quota_bytes_used,starred,file_extension,sharing_user(id,focus_user_id,is_me,type),spaces,trashed,restricted,version,viewed,team_drive_id,has_own_permissions,create_date_millis,trashing_user(id,focus_user_id,is_me,type),trashed_date_millis,has_visitor_permissions,abuse_is_appealable,spam_metadata(marked_as_spam_date_millis),applied_labels,folder_color,folder_features,recency_date_millis,recency_date_reason,action_item,organization_display_name),continuation_token,search_response_metadata(incomplete_search,moonshine_item_ids,query_suggestions(spell_response,nlp_response))"
)

type folderListing struct {
	FileList             []folderFileList
	FolderTree           folderTree
	SearchedFolder       *drive.File
	TotalNumberOfFiles   int
	TotalNumberOfFolders int
}

type folderTree struct {
	Folders []string
	Names   []string
}

type folderFileList struct {
	Files      []*drive.File
	FolderTree []string
}

type folderDownloadJob struct {
	File        *drive.File
	Relative    string
	Destination string
}

type publicDriveFrontendClient struct {
	APIKey string
	Client *http.Client
}

func fallbackPublicFolderRoot(id string) *drive.File {
	return &drive.File{Id: id, Name: id, MimeType: "application/vnd.google-apps.folder"}
}

func newFolderListingFromRoot(root *drive.File) *folderListing {
	return &folderListing{
		SearchedFolder:       root,
		TotalNumberOfFolders: 1,
		FolderTree: folderTree{
			Folders: []string{root.Id},
			Names:   []string{root.Name},
		},
	}
}

// mime2ext : Convert mimeType to extension.
func mime2ext(mime string) string {
	var obj map[string]interface{}
	json.Unmarshal([]byte(mimeVsEx), &obj)
	res, _ := obj[mime].(string)
	return res
}

func newFolderDownloadPara(p *para, file *drive.File) *para {
	downloadPara := p.clone()
	downloadPara.WorkDir = p.WorkDir
	downloadPara.Filename = firstNonEmpty(p.Filename, file.Name)
	downloadPara.Size = file.Size
	downloadPara.Task = p.Task
	downloadPara.ID = file.Id
	return downloadPara
}

// downloadFileByAPIKey : Download file using API key.
func (p *para) downloadFileByAPIKey(file *drive.File) error {
	u, err := url.Parse(driveAPI)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, file.Id)
	q := u.Query()
	q.Set("key", p.APIKey)
	if strings.Contains(file.MimeType, "application/vnd.google-apps") {
		u.Path = path.Join(u.Path, "export")
		q.Set("mimeType", file.WebViewLink)
	} else {
		q.Set("alt", "media")
		q.Set("supportsAllDrives", "true")
	}
	u.RawQuery = q.Encode()
	downloadPara := newFolderDownloadPara(p, file)
	timeOut := func(size int64) int64 {
		if size == 0 {
			switch {
			case size < 100000000:
				return 3600
			case size > 100000000:
				return 0
			}
		}
		return 0
	}(file.Size)
	downloadPara.Client, err = downloadPara.TransportConfig.newHTTPClient(nil)
	if err != nil {
		return err
	}
	if downloadPara.Client.Timeout == 0 {
		downloadPara.Client.Timeout = time.Duration(timeOut) * time.Second
	}
	res, err := downloadPara.fetch(u.String())
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		r, err := io.ReadAll(res.Body)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if downloadPara.SkipError {
			downloadPara.printf("!! Downloading '%s' (fileId: %s) was skipped by an error. Status code is %d.\n", file.Name, file.Id, res.StatusCode)
			if downloadPara.Task != nil {
				downloadPara.Task.MarkSkipped(fmt.Sprintf("HTTP %d", res.StatusCode))
			}
			return nil
		}
		return fmt.Errorf("%s", r)
	}
	return downloadPara.saveFile(res)
}

func (p *para) downloadFileByPublicLink(file *drive.File) error {
	downloadPara := newFolderDownloadPara(p, file)
	url, kind, err := buildPublicFolderDownloadURL(file)
	if err != nil {
		return err
	}
	downloadPara.URL = url
	downloadPara.Kind = kind
	return downloadPara.downloadResolvedURL()
}

func (p *para) downloadFolderFile(file *drive.File) error {
	if p.APIKey != "" {
		return p.downloadFileByAPIKey(file)
	}
	return p.downloadFileByPublicLink(file)
}

// makeFileByCondition : Make file by condition.
func (p *para) makeFileByCondition(file *drive.File) error {
	if p.DryRun {
		return p.downloadFolderFile(file)
	}
	filename := firstNonEmpty(p.Filename, file.Name)
	if er := chkFile(filepath.Join(p.WorkDir, filename)); er {
		if !p.OverWrite && !p.Skip {
			return fmt.Errorf("'%s' is existing. If you want to overwrite, please use an option '--overwrite'", filepath.Join(p.WorkDir, filename))
		}
		if p.OverWrite && !p.Skip {
			return p.downloadFolderFile(file)
		}
		if !p.Disp && p.Skip {
			p.printf("Downloading '%s' was skipped because of existing.\n", filename)
			if p.Task != nil {
				p.Task.MarkSkipped("existing file")
			}
		}
	} else {
		return p.downloadFolderFile(file)
	}
	return nil
}

// makeDir : Make a directory by checking duplication.
func (p *para) makeDir(folder string) error {
	if er := chkFile(folder); !er {
		if err := os.MkdirAll(folder, 0777); err != nil {
			return err
		}
	} else {
		if !p.OverWrite && !p.Skip {
			return fmt.Errorf("'%s' is existing. If you want to overwrite, please use an option '--overwrite'", folder)
		}
	}
	return nil
}

// chkFile : Check the existence of file and directory in local PC.
func chkFile(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// makeDirByCondition : Make directory by condition.
func (p *para) makeDirByCondition(dir string) error {
	if p.DryRun {
		return nil
	}
	var err error
	if er := chkFile(dir); er {
		if !p.OverWrite && !p.Skip {
			return fmt.Errorf("'%s' is existing. If you want to overwrite, please use option '--overwrite' or '--skip'", dir)
		}
		if p.OverWrite && !p.Skip {
			if err = p.makeDir(dir); err != nil {
				return err
			}
		}
		if !p.Disp && p.Skip {
			p.printf("Creating '%s' was skipped because of existing.\n", dir)
		}
	} else {
		p.statusf("Creating directory path: %s", dir)
		if err = p.makeDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// initDownload : Download files by Drive API using API key.
func (p *para) initDownload(fileList *folderListing) error {
	var err error
	if !p.Disp {
		p.printf("Download files from a folder '%s'.\n", fileList.SearchedFolder.Name)
		p.printf("There are %d files and %d folders in the folder.\n", fileList.TotalNumberOfFiles, fileList.TotalNumberOfFolders-1)
		p.printf("Starting download.\n")
	}
	idToName := map[string]interface{}{}
	for i, e := range fileList.FolderTree.Folders {
		idToName[e] = fileList.FolderTree.Names[i]
	}
	createdDirs := map[string]bool{}
	var jobs []folderDownloadJob
	for _, e := range fileList.FileList {
		path := p.WorkDir
		folderTree := append([]string(nil), e.FolderTree...)
		if p.Notcreatetopdirectory {
			folderTree = append(folderTree[:0], folderTree[1:]...)
		}
		var relativeParts []string
		for _, dir := range folderTree {
			path = filepath.Join(path, idToName[dir].(string))
			relativeParts = append(relativeParts, idToName[dir].(string))
		}
		if path != p.WorkDir && !createdDirs[path] {
			createdDirs[path] = true
			err = p.makeDirByCondition(path)
			if err != nil {
				return err
			}
		}
		for _, file := range e.Files {
			if file.MimeType != "application/vnd.google-apps.script" {
				relativeName := filepath.Join(append(append([]string(nil), relativeParts...), file.Name)...)
				jobs = append(jobs, folderDownloadJob{File: file, Relative: relativeName, Destination: path})
			} else {
				if !p.Disp {
					p.printf("'%s' is a project file. Project files cannot be downloaded from folder links.\n", file.Name)
				}
				if p.Runtime != nil {
					task := p.Runtime.newTask(filepath.Join(append(append([]string(nil), relativeParts...), file.Name)...), file.Id)
					task.MarkSkipped("project file")
				}
			}
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	workers := p.MaxConcurrency
	if workers < 1 {
		workers = 1
	}
	jobCh := make(chan folderDownloadJob)
	ctx := p.requestContext()
	errCh := make(chan error, len(jobs))
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
					jobPara.WorkDir = job.Destination
					jobPara.Filename = job.File.Name
					jobPara.Size = job.File.Size
					if jobPara.Runtime != nil {
						jobPara.Task = jobPara.Runtime.newTask(job.Relative, job.File.Id)
						jobPara.Task.SetTotal(job.File.Size)
					}
					if err := jobPara.makeFileByCondition(job.File); err != nil {
						if jobPara.Task != nil {
							jobPara.Task.MarkFailed(err)
						}
						errCh <- err
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
	close(errCh)
	if err := ctx.Err(); err != nil {
		return err
	}
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// defFormat : Default download format
func defFormat(mime string) string {
	var df map[string]interface{}
	json.Unmarshal([]byte(defaultformat), &df)
	dmime, _ := df[mime].(string)
	return dmime
}

// extToMime : Convert from extension to mimeType of the file on Local.
func extToMime(ext string) string {
	var fm map[string]interface{}
	json.Unmarshal([]byte(extVsmime), &fm)
	st, _ := fm[strings.Replace(strings.ToLower(ext), ".", "", 1)].(string)
	return st
}

func buildPublicFolderDownloadURL(file *drive.File) (string, string, error) {
	if file == nil {
		return "", "", fmt.Errorf("file is required")
	}
	if !strings.Contains(file.MimeType, "application/vnd.google-apps") {
		return anyurl + "&id=" + file.Id, "file", nil
	}
	outMime := file.WebViewLink
	if outMime == "" {
		outMime = defFormat(file.MimeType)
	}
	format := strings.TrimPrefix(mime2ext(outMime), ".")
	if format == "" {
		return "", "", fmt.Errorf("unable to determine export format for '%s'", file.Name)
	}
	specs := map[string]struct {
		Kind      string
		PathStyle bool
	}{
		"application/vnd.google-apps.document":     {Kind: "document"},
		"application/vnd.google-apps.spreadsheet":  {Kind: "spreadsheets"},
		"application/vnd.google-apps.presentation": {Kind: "presentation", PathStyle: true},
		"application/vnd.google-apps.drawing":      {Kind: "drawings"},
	}
	spec, ok := specs[file.MimeType]
	if !ok {
		return "", "", fmt.Errorf("public folder downloads do not support '%s'", file.MimeType)
	}
	if spec.PathStyle {
		return docutl + spec.Kind + "/d/" + file.Id + "/export/" + format, spec.Kind, nil
	}
	return docutl + spec.Kind + "/d/" + file.Id + "/export?format=" + format, spec.Kind, nil
}

// dupChkFoldersFiles : Check duplication of folder names and filenames.
func (p *para) dupChkFoldersFiles(fileList *folderListing) {
	dupChk1 := map[string]bool{}
	cnt1 := 2
	for i, folderName := range fileList.FolderTree.Names {
		if !dupChk1[folderName] {
			dupChk1[folderName] = true
		} else {
			fileList.FolderTree.Names[i] = folderName + "_" + strconv.Itoa(cnt1)
		}
	}
	extt := strings.ToLower(p.Ext)
	for i, list := range fileList.FileList {
		if len(list.Files) > 0 {
			dupChk2 := map[string]bool{}
			cnt2 := 2
			for j, file := range list.Files {
				if !dupChk2[file.Name] {
					dupChk2[file.Name] = true
				} else {
					ext := filepath.Ext(file.Name)
					if ext != "" {
						fileList.FileList[i].Files[j].Name = file.Name[0:len(file.Name)-len(ext)] + "_" + strconv.Itoa(cnt2) + ext
					} else {
						fileList.FileList[i].Files[j].Name = file.Name + "_" + strconv.Itoa(cnt2)
					}
					cnt2++
				}
				mime := defFormat(file.MimeType)
				if extt != "" {
					if mime != "" {
						cmime := func() string {
							if (extt == "txt" || extt == "text") && file.MimeType == "application/vnd.google-apps.spreadsheet" {
								return extToMime("csv")
							} else if extt == "zip" && file.MimeType == "application/vnd.google-apps.presentation" {
								return extToMime("pptx")
							}
							return extToMime(extt)
						}()
						if cmime != "" {
							fileList.FileList[i].Files[j].WebViewLink = cmime // Substituting as OutMimeType
						} else {
							fileList.FileList[i].Files[j].WebViewLink = mime // Substituting as OutMimeType
						}
					}
				} else {
					fileList.FileList[i].Files[j].WebViewLink = mime // Substituting as OutMimeType
				}
				if file.MimeType != "application/vnd.google-apps.script" {
					ext := filepath.Ext(file.Name)
					if ext == "" {
						fileList.FileList[i].Files[j].Name += mime2ext(fileList.FileList[i].Files[j].WebViewLink)
					}
				}
			}
		}
	}
}

func buildPublicFolderGetRequestBody(id string) []interface{} {
	return []interface{}{
		[]interface{}{id},
		[]interface{}{nil, nil, nil, nil, nil, []interface{}{2, 5}},
	}
}

func buildPublicFolderListRequestBody(parentID, pageToken string) []interface{} {
	filter := make([]interface{}, 44)
	filter[4] = 0
	filter[19] = ""
	filter[21] = 0
	filter[24] = []interface{}{4, 1, 1}
	filter[35] = []interface{}{[]interface{}{1}}
	filter[43] = []interface{}{[]interface{}{parentID}}
	return []interface{}{
		filter,
		[]interface{}{50, pageToken, []interface{}{2, 5}},
	}
}

func (c *publicDriveFrontendClient) getItem(ctx context.Context, id string) (*drive.File, error) {
	body, err := c.postJSON(ctx, "/v1/items:get", buildPublicFolderGetRequestBody(id))
	if err != nil {
		return nil, err
	}
	return parsePublicFolderGetResponse(body)
}

func (c *publicDriveFrontendClient) listItems(ctx context.Context, parentID, pageToken string) ([]*drive.File, string, error) {
	body, err := c.postJSON(ctx, "/v1/items:list", buildPublicFolderListRequestBody(parentID, pageToken))
	if err != nil {
		return nil, "", err
	}
	return parsePublicFolderListResponse(body)
}

func (c *publicDriveFrontendClient) getAccessibleRoot(ctx context.Context, id string) (*drive.File, error) {
	root, err := c.getItem(ctx, id)
	if err == nil {
		return root, nil
	}
	if _, _, listErr := c.listItems(ctx, id, ""); listErr == nil {
		return fallbackPublicFolderRoot(id), nil
	}
	return nil, err
}

func publicDriveFrontendFieldMask(endpoint string) string {
	switch endpoint {
	case "/v1/items:get":
		return publicDriveFieldMaskGet
	case "/v1/items:list":
		return publicDriveFieldMaskList
	default:
		return ""
	}
}

func (c *publicDriveFrontendClient) postJSON(ctx context.Context, endpoint string, payload interface{}) ([]byte, error) {
	if c == nil || c.Client == nil {
		return nil, fmt.Errorf("public drive frontend client is not initialized")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(publicDriveFrontendAPI + endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("key", c.APIKey)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json+protobuf")
	req.Header.Set("Origin", "https://drive.google.com")
	req.Header.Set("Referer", "https://drive.google.com/")
	if fieldMask := publicDriveFrontendFieldMask(endpoint); fieldMask != "" {
		req.Header.Set("X-Goog-FieldMask", fieldMask)
	}
	res, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public folder listing failed: HTTP %d: %s", res.StatusCode, truncateHTTPBody(body, 256))
	}
	return body, nil
}

func parsePublicFolderGetResponse(body []byte) (*drive.File, error) {
	var payload []interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	items, ok := interfaceSliceAt(payload, 0)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("unexpected public folder get response")
	}
	entry, ok := interfaceSlice(items[0])
	if !ok || len(entry) < 2 {
		return nil, fmt.Errorf("unexpected public folder get entry")
	}
	rawItem, ok := interfaceSlice(entry[1])
	if !ok {
		return nil, fmt.Errorf("unexpected public folder item payload")
	}
	return publicDriveFileFromItem(rawItem)
}

func parsePublicFolderListResponse(body []byte) ([]*drive.File, string, error) {
	var payload []interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", err
	}
	rawItems, ok := interfaceSliceAt(payload, 0)
	if !ok {
		return nil, "", fmt.Errorf("unexpected public folder list response")
	}
	files := make([]*drive.File, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := interfaceSlice(raw)
		if !ok {
			continue
		}
		file, err := publicDriveFileFromItem(item)
		if err != nil {
			return nil, "", err
		}
		files = append(files, file)
	}
	nextToken, _ := stringAt(payload, 1)
	return files, nextToken, nil
}

func publicDriveFileFromItem(item []interface{}) (*drive.File, error) {
	id, ok := stringAt(item, 0)
	if !ok || id == "" {
		return nil, fmt.Errorf("public drive item is missing an id")
	}
	name, _ := stringAt(item, 2)
	mimeType, _ := stringAt(item, 3)
	size, _ := int64At(item, 13)
	parents := stringSliceAt(item, 1)
	return &drive.File{Id: id, Name: name, MimeType: mimeType, Size: size, Parents: parents}, nil
}

func interfaceSliceAt(values []interface{}, index int) ([]interface{}, bool) {
	if index < 0 || index >= len(values) {
		return nil, false
	}
	return interfaceSlice(values[index])
}

func interfaceSlice(value interface{}) ([]interface{}, bool) {
	values, ok := value.([]interface{})
	return values, ok
}

func stringAt(values []interface{}, index int) (string, bool) {
	if index < 0 || index >= len(values) || values[index] == nil {
		return "", false
	}
	value, ok := values[index].(string)
	return value, ok
}

func int64At(values []interface{}, index int) (int64, bool) {
	if index < 0 || index >= len(values) || values[index] == nil {
		return 0, false
	}
	switch value := values[index].(type) {
	case float64:
		return int64(value), true
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}

func stringSliceAt(values []interface{}, index int) []string {
	raw, ok := interfaceSliceAt(values, index)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func truncateHTTPBody(body []byte, limit int) string {
	text := strings.TrimSpace(string(body))
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func extractPublicDriveFolderAPIKeys(page string) []string {
	matches := regexp.MustCompile(`AIza[0-9A-Za-z_-]{20,}`).FindAllString(page, -1)
	seen := map[string]bool{}
	keys := make([]string, 0, len(matches))
	for _, key := range matches {
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

func (p *para) discoverPublicFolderAPIKeys(client *http.Client, folderID string) ([]string, error) {
	p.statusf("Fetching shared folder page to discover public listing API keys: %s", folderID)
	req, err := http.NewRequestWithContext(p.requestContext(), http.MethodGet, publicDriveFoldersPageBase+folderID, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := p.detectGoogleLoginRequirement(res); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return extractPublicDriveFolderAPIKeys(string(body)), nil
}

// getFilesFromFolder: This method is the main method for downloading all files in a shared folder.
func (p *para) getFilesFromFolder() error {
	var (
		fileList *folderListing
		err      error
	)
	if p.APIKey != "" {
		p.statusf("Starting shared folder listing via Drive API: %s", p.SearchID)
		srv, driveErr := p.newDriveService(p.requestContext())
		if driveErr != nil {
			return driveErr
		}
		fileList, err = p.listFolderFiles(srv, p.SearchID)
	} else {
		p.statusf("Starting public shared folder listing without API key: %s", p.SearchID)
		fileList, err = p.listPublicFolderFiles(p.SearchID)
	}
	if err != nil {
		return err
	}
	if p.ShowFileInf {
		r, err := json.Marshal(fileList)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", r)
		return nil
	}
	p.dupChkFoldersFiles(fileList)
	if err := p.initDownload(fileList); err != nil {
		return err
	}
	return nil
}

func (p *para) listPublicFolderFiles(rootID string) (*folderListing, error) {
	p.statusf("Initializing HTTP client for public folder listing")
	client, err := p.TransportConfig.newHTTPClient(nil)
	if err != nil {
		return nil, err
	}
	frontend := &publicDriveFrontendClient{APIKey: publicDriveFrontendAPIKey, Client: client}
	p.statusf("Requesting public folder root metadata: %s", rootID)
	root, err := frontend.getAccessibleRoot(p.requestContext(), rootID)
	if err != nil {
		keys, discoverErr := p.discoverPublicFolderAPIKeys(client, rootID)
		if discoverErr != nil {
			return nil, discoverErr
		}
		for _, key := range keys {
			if key == "" || key == frontend.APIKey {
				continue
			}
			frontend.APIKey = key
			p.statusf("Retrying public folder root metadata with discovered API key")
			root, err = frontend.getAccessibleRoot(p.requestContext(), rootID)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, err
		}
	}
	listing := newFolderListingFromRoot(root)
	if err := p.walkPublicFolderFiles(frontend, root.Id, []string{root.Id}, listing); err != nil {
		return nil, err
	}
	return listing, nil
}

func (p *para) listFolderFiles(srv *drive.Service, rootID string) (*folderListing, error) {
	p.statusf("Requesting root folder metadata via Drive API: %s", rootID)
	root, err := srv.Files.Get(rootID).Fields("id,name").SupportsAllDrives(true).Do()
	if err != nil {
		return nil, err
	}
	listing := newFolderListingFromRoot(root)
	if err := p.walkFolderFiles(srv, root.Id, []string{root.Id}, listing); err != nil {
		return nil, err
	}
	return listing, nil
}

func (p *para) walkFolderFiles(srv *drive.Service, folderID string, tree []string, listing *folderListing) error {
	pageToken := ""
	for {
		if err := p.contextErr(); err != nil {
			return err
		}
		p.statusf("Requesting Drive API folder listing: folder=%s page=%s", folderID, firstNonEmpty(pageToken, "first"))
		call := srv.Files.List().
			Q(fmt.Sprintf("'%s' in parents and trashed = false", folderID)).
			Fields("nextPageToken,files(id,name,mimeType,size,webContentLink,webViewLink)").
			IncludeItemsFromAllDrives(true).
			SupportsAllDrives(true).
			PageSize(1000)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return err
		}
		entry := folderFileList{FolderTree: append([]string(nil), tree...)}
		for _, file := range res.Files {
			if file.MimeType == "application/vnd.google-apps.folder" {
				listing.TotalNumberOfFolders++
				listing.FolderTree.Folders = append(listing.FolderTree.Folders, file.Id)
				listing.FolderTree.Names = append(listing.FolderTree.Names, file.Name)
				nextTree := append(append([]string(nil), tree...), file.Id)
				if err := p.walkFolderFiles(srv, file.Id, nextTree, listing); err != nil {
					return err
				}
				continue
			}
			if len(p.InputtedMimeType) == 0 || containsMimeType(p.InputtedMimeType, file.MimeType) {
				entry.Files = append(entry.Files, file)
				listing.TotalNumberOfFiles++
			}
		}
		if len(entry.Files) > 0 {
			listing.FileList = append(listing.FileList, entry)
		}
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}
	return nil
}

func (p *para) walkPublicFolderFiles(frontend *publicDriveFrontendClient, folderID string, tree []string, listing *folderListing) error {
	pageToken := ""
	for {
		if err := p.contextErr(); err != nil {
			return err
		}
		p.statusf("Requesting public folder listing: folder=%s page=%s", folderID, firstNonEmpty(pageToken, "first"))
		files, nextPageToken, err := frontend.listItems(p.requestContext(), folderID, pageToken)
		if err != nil {
			return err
		}
		entry := folderFileList{FolderTree: append([]string(nil), tree...)}
		for _, file := range files {
			if file.MimeType == "application/vnd.google-apps.folder" {
				listing.TotalNumberOfFolders++
				listing.FolderTree.Folders = append(listing.FolderTree.Folders, file.Id)
				listing.FolderTree.Names = append(listing.FolderTree.Names, file.Name)
				nextTree := append(append([]string(nil), tree...), file.Id)
				if err := p.walkPublicFolderFiles(frontend, file.Id, nextTree, listing); err != nil {
					return err
				}
				continue
			}
			if len(p.InputtedMimeType) == 0 || containsMimeType(p.InputtedMimeType, file.MimeType) {
				entry.Files = append(entry.Files, file)
				listing.TotalNumberOfFiles++
			}
		}
		if len(entry.Files) > 0 {
			listing.FileList = append(listing.FileList, entry)
		}
		if nextPageToken == "" {
			break
		}
		pageToken = nextPageToken
	}
	return nil
}

func containsMimeType(values []string, mimeType string) bool {
	for _, value := range values {
		if value == mimeType {
			return true
		}
	}
	return false
}
