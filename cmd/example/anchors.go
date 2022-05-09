package main

import "regexp"

const (
	exampleStart = "<!-- START EXAMPLE -->"
	exampleEnd   = "<!-- END EXAMPLE -->"
)

var exampleBlockRegexp = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(exampleStart) + `(.*?)` + regexp.QuoteMeta(exampleEnd))
