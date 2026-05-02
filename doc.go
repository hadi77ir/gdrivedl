/*
Package main (doc.go) :
This is a CLI tool to download shared files from Google Drive.

We have already known that the shared files on Google Drive can be downloaded without the authorization. But when the size of file becomes large (about 40MB), it requires a little ingenuity to download the file. It requires to access 2 times to Google Drive. At 1st access, it retrieves a cookie and a code for downloading. At 2nd access, the file is downloaded using the cookie and code. I created this process as a CLI tool. This tool has the following features.

- Use suitable process for size and type of file.

- Retrieve filename and mimetype from response header.

- Can download all shared files except for project files.

- gdrivedl can download all files in a public shared folder without an API key.

- By using API key, gdrivedl can run the resumable download of files.

---------------------------------------------------------------

# How to Install
Download an executable file of gdrivedl from https://github.com/hadi77ir/gdrivedl/releases

or

Use go install.

$ go install github.com/hadi77ir/gdrivedl@latest

# Usage
You can use this just after you download or install gdrivedl. You are not required to do like OAuth2 process.

$ gdrivedl -u [URL of shared file on Google Drive]

You can download all files in a public shared folder without an API key.

$ gdrivedl -u [URL of shared folder on Google Drive]

If you use API key, you can also use the Drive API based folder flow.

$ gdrivedl -u [URL of shared folder on Google Drive] -key [API key]

---------------------------------------------------------------
*/
package gdrivedl
