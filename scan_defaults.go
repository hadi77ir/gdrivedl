package gdrivedl

import (
	_ "embed"
	"strings"
)

var (
	//go:embed assets/known-domains.txt
	embeddedKnownDomains string

	//go:embed assets/known-ip-ranges.txt
	embeddedKnownIPRanges string
)

func defaultScanDomainLoader() ([]string, error) {
	return readHostnames(strings.NewReader(embeddedKnownDomains))
}

func defaultScanIPSpecLoader() ([]string, error) {
	return readIPSpecs(strings.NewReader(embeddedKnownIPRanges))
}
