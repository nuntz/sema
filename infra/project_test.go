package main

import (
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
)

// Pulumi Cloud copies a project's description into the stack's pulumi:description tag, which it
// caps at 256 characters. A local backend has no such cap, so only `pulumi stack init` against
// the Cloud would catch a longer one.
func TestProjectDescriptionsFitPulumiCloudsTagLimit(t *testing.T) {
	const maxTagValue = 256
	nested, err := filepath.Glob("*/Pulumi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range append([]string{"Pulumi.yaml"}, nested...) {
		project, err := workspace.LoadProject(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		if project.Description == nil {
			continue
		}
		if n := utf8.RuneCountInString(*project.Description); n > maxTagValue {
			t.Errorf("%s: description is %d characters; Pulumi Cloud allows %d", path, n, maxTagValue)
		}
	}
}
