package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	drive "google.golang.org/api/drive/v3"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestBuildPublicFolderRequestBodies(t *testing.T) {
	getBody := buildPublicFolderGetRequestBody("root123")
	if got, ok := interfaceSliceAt(getBody, 0); !ok || len(got) != 1 || got[0] != "root123" {
		t.Fatalf("buildPublicFolderGetRequestBody() = %#v", getBody)
	}
	meta, ok := interfaceSliceAt(getBody, 1)
	if !ok || len(meta) != 6 {
		t.Fatalf("get request metadata = %#v", meta)
	}

	listBody := buildPublicFolderListRequestBody("parent123", "token456")
	filter, ok := interfaceSliceAt(listBody, 0)
	if !ok || len(filter) != 44 {
		t.Fatalf("buildPublicFolderListRequestBody() filter = %#v", filter)
	}
	if got, ok := int64At(filter, 4); !ok || got != 0 {
		t.Fatalf("filter[4] = %v", filter[4])
	}
	if got, ok := stringAt(filter, 19); !ok || got != "" {
		t.Fatalf("filter[19] = %#v", filter[19])
	}
	if got, ok := int64At(filter, 21); !ok || got != 0 {
		t.Fatalf("filter[21] = %#v", filter[21])
	}
	if parents := stringSliceAt(mustInterfaceSlice(t, filter[43]), 0); len(parents) != 1 || parents[0] != "parent123" {
		t.Fatalf("parents = %#v", filter[43])
	}
	options, ok := interfaceSliceAt(listBody, 1)
	if !ok || len(options) != 3 {
		t.Fatalf("list options = %#v", options)
	}
	if got, ok := stringAt(options, 1); !ok || got != "token456" {
		t.Fatalf("page token = %#v", options)
	}
}

func TestParsePublicFolderResponses(t *testing.T) {
	getResponse := `[[[[],["root123",null,"Root Folder","application/vnd.google-apps.folder",0,null,0,0,0,1777645819206,1777675289082,null,null,null]]]]`
	root, err := parsePublicFolderGetResponse([]byte(getResponse))
	if err != nil {
		t.Fatalf("parsePublicFolderGetResponse() error = %v", err)
	}
	if root.Id != "root123" || root.Name != "Root Folder" || root.MimeType != "application/vnd.google-apps.folder" {
		t.Fatalf("root = %#v", root)
	}

	listResponse := `[[["file123",["root123"],"alpha.bin","application/octet-stream",0,null,0,0,0,1777646030636,1777645831000,null,null,10000000],["folder456",["root123"],"nested","application/vnd.google-apps.folder",0,null,0,0,0,1777646030636,1777645831000,null,null,null]],"~!!~next-page-token",[null,0]]`
	files, nextToken, err := parsePublicFolderListResponse([]byte(listResponse))
	if err != nil {
		t.Fatalf("parsePublicFolderListResponse() error = %v", err)
	}
	if nextToken != "~!!~next-page-token" {
		t.Fatalf("next token = %q", nextToken)
	}
	if len(files) != 2 {
		t.Fatalf("file count = %d, want 2", len(files))
	}
	if files[0].Id != "file123" || files[0].Size != 10000000 || len(files[0].Parents) != 1 || files[0].Parents[0] != "root123" {
		t.Fatalf("first file = %#v", files[0])
	}
	if files[1].MimeType != "application/vnd.google-apps.folder" {
		t.Fatalf("second file = %#v", files[1])
	}
}

func TestBuildPublicFolderDownloadURL(t *testing.T) {
	url, kind, err := buildPublicFolderDownloadURL(&driveFileFixture)
	if err != nil {
		t.Fatalf("buildPublicFolderDownloadURL(binary) error = %v", err)
	}
	if kind != "file" || !strings.Contains(url, "export=download&id=file123") {
		t.Fatalf("binary url = %q kind=%q", url, kind)
	}

	doc := driveFileFixture
	doc.Id = "doc123"
	doc.MimeType = "application/vnd.google-apps.document"
	doc.WebViewLink = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	url, kind, err = buildPublicFolderDownloadURL(&doc)
	if err != nil {
		t.Fatalf("buildPublicFolderDownloadURL(document) error = %v", err)
	}
	if kind != "document" || !strings.Contains(url, "/document/d/doc123/export?format=docx") {
		t.Fatalf("document url = %q kind=%q", url, kind)
	}

	unsupported := driveFileFixture
	unsupported.MimeType = "application/vnd.google-apps.form"
	unsupported.WebViewLink = "application/zip"
	if _, _, err := buildPublicFolderDownloadURL(&unsupported); err == nil || !strings.Contains(err.Error(), "do not support") {
		t.Fatalf("unsupported mime error = %v", err)
	}
}

func TestPublicDriveFrontendClientGetAccessibleRootFallsBackToList(t *testing.T) {
	var paths []string
	var listFieldMask string
	client := &publicDriveFrontendClient{
		APIKey: "test-key",
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.URL.Path)
			switch req.URL.Path {
			case "/v1/items:get":
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":"bad request"}`)),
					Request:    req,
				}, nil
			case "/v1/items:list":
				listFieldMask = req.Header.Get("X-Goog-FieldMask")
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`[[["file123",["nested123"],"alpha.bin","application/octet-stream",0,null,0,0,0,1777646030636,1777645831000,null,null,10000000]],"",[null,0]]`)),
					Request:    req,
				}, nil
			default:
				return nil, nil
			}
		})},
	}

	root, err := client.getAccessibleRoot(context.Background(), "nested123")
	if err != nil {
		t.Fatalf("getAccessibleRoot() error = %v", err)
	}
	if root.Id != "nested123" || root.Name != "nested123" || root.MimeType != "application/vnd.google-apps.folder" {
		t.Fatalf("fallback root = %#v", root)
	}
	if len(paths) != 2 || paths[0] != "/v1/items:get" || paths[1] != "/v1/items:list" {
		t.Fatalf("request paths = %#v", paths)
	}
	if listFieldMask != publicDriveFieldMaskList {
		t.Fatalf("list field mask = %q", listFieldMask)
	}
}

func TestNewFolderListingFromRoot(t *testing.T) {
	root := fallbackPublicFolderRoot("nested123")
	listing := newFolderListingFromRoot(root)
	if listing.SearchedFolder != root {
		t.Fatal("listing should keep the root file")
	}
	if listing.TotalNumberOfFolders != 1 || len(listing.FolderTree.Folders) != 1 || listing.FolderTree.Folders[0] != "nested123" {
		t.Fatalf("listing = %#v", listing)
	}
}

func TestPublicDriveFrontendFieldMask(t *testing.T) {
	if got := publicDriveFrontendFieldMask("/v1/items:get"); got != publicDriveFieldMaskGet {
		t.Fatalf("get field mask = %q", got)
	}
	if got := publicDriveFrontendFieldMask("/v1/items:list"); got != publicDriveFieldMaskList {
		t.Fatalf("list field mask = %q", got)
	}
	if got := publicDriveFrontendFieldMask("/v1/unknown"); got != "" {
		t.Fatalf("unknown field mask = %q", got)
	}
}

func TestExtractPublicDriveFolderAPIKeys(t *testing.T) {
	page := `"AIzaSyAAAABBBBBCCCCCDDDDDEEEEEFFFFF","AIzaSy1111122222333334444455555aaaaa","AIzaSyAAAABBBBBCCCCCDDDDDEEEEEFFFFF"`
	keys := extractPublicDriveFolderAPIKeys(page)
	if len(keys) != 2 {
		t.Fatalf("keys = %#v", keys)
	}
	if keys[0] == keys[1] {
		t.Fatalf("keys should be unique: %#v", keys)
	}
}

var driveFileFixture = drive.File{
	Id:       "file123",
	Name:     "alpha.bin",
	MimeType: "application/octet-stream",
	Size:     10000000,
}

func mustInterfaceSlice(t *testing.T, value interface{}) []interface{} {
	t.Helper()
	values, ok := value.([]interface{})
	if !ok {
		t.Fatalf("value is not []interface{}: %#v", value)
	}
	return values
}
