/*
Package gdrivedl provides a Go library and CLI for public Google Drive downloads.

The package supports:

- Public file downloads without OAuth.
- Public folder downloads without an API key.
- Optional resumable downloads when an API key is available.
- Transport customization such as proxies, domain fronting, uTLS profiles,
  resolve-to IP overrides, and HTTP version preferences.
- Connectivity probing and phase-selectable scan helpers based on
  https://gstatic.com/generate_204.
- Safe merging of split chunk folders.

CLI usage starts with subcommands:

	gdrivedl get   - download files, folders, or URL lists
	gdrivedl scan  - probe viable direct and fronted routes
	gdrivedl test  - send one transport probe
	gdrivedl merge - combine chunk files into one output file

Each CLI subcommand accepts --config and loads defaults from
$XDG_CONFIG_DIR/gdrivedl.yml, then $XDG_CONFIG_HOME/gdrivedl.yml,
when those files exist.
*/
package gdrivedl
