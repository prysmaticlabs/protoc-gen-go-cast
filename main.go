package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	gengo "google.golang.org/protobuf/cmd/protoc-gen-go/internal_gengo"
	"google.golang.org/protobuf/compiler/protogen"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Fprintf(os.Stderr, "%v\n", filepath.Base(os.Args[0]))
		os.Exit(0)
	}

	var (
		flags        flag.FlagSet
		plugins      = flags.String("plugins", "", "list of plugins to enable (supported values: grpc)")
		importPrefix = flags.String("import_prefix", "", "prefix to prepend to import paths")
		silent       = flags.Bool("silent", false, "silence the output")
	)
	importRewriteFunc := func(importPath protogen.GoImportPath) protogen.GoImportPath {
		switch importPath {
		case "context", "fmt", "math":
			return importPath
		}
		if *importPrefix != "" {
			return protogen.GoImportPath(*importPrefix) + importPath
		}
		return importPath
	}
	protogen.Options{
		ParamFunc:         flags.Set,
		ImportRewriteFunc: importRewriteFunc,
	}.Run(func(gen *protogen.Plugin) error {
		if *silent {
			log.SetOutput(io.Discard)
		}
		grpc := false
		for _, plugin := range strings.Split(*plugins, ",") {
			log.Println(plugin)
			switch plugin {
			case "grpc":
				grpc = true
			case "":
			default:
				return fmt.Errorf("protoc-gen-go: unknown plugin %q", plugin)
			}
		}
		var allExtensions []*protogen.Extension
		for _, f := range gen.Files {
			allExtensions = append(allExtensions, f.Extensions...)
		}
		extensionNames := make([]string, len(allExtensions))
		for i, ee := range allExtensions {
			extensionNames[i] = string(ee.Desc.Name())
		}
		log.Printf("Casting for %d extensions: %s\n", len(allExtensions), strings.Join(extensionNames, ", "))
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			gennedFile := gengo.GenerateFile(gen, f)
			if grpc {
				GenerateFileContent(gen, f, gennedFile)
			}
			GenerateCastedFile(gen, gennedFile, f, allExtensions)
		}
		gen.SupportedFeatures = gengo.SupportedFeatures
		return nil
	})
}
