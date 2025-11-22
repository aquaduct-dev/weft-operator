//go:build tools
// +build tools

package tools

import (
	_ "github.com/fatih/color"
	_ "github.com/gobuffalo/flect"
	_ "github.com/spf13/cobra"
	_ "golang.org/x/tools/go/packages"
	_ "gopkg.in/yaml.v2"
	_ "k8s.io/code-generator"
	_ "sigs.k8s.io/controller-tools/pkg/markers"
)
