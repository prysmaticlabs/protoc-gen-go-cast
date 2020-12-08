package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

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
		plugins      = flags.String("plugins", "", "deprecated option")
		importPrefix = flags.String("import_prefix", "", "deprecated option")
	)
	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(func(gen *protogen.Plugin) error {
		if *plugins != "" {
			return errors.New("protoc-gen-go-cast: plugins are not supported; use 'protoc --go-grpc_out=...' to generate gRPC")
		}
		if *importPrefix != "" {
			return errors.New("protoc-gen-go-cast: import_prefix is not supported")
		}
		var allExtensions []*protogen.Extension
		for _, f := range gen.Files {
			allExtensions = append(allExtensions, f.Extensions...)
		}
		log.Println(len(allExtensions))
		for _, f := range gen.Files {
			if f.Generate {
				gennedFile := gengo.GenerateFile(gen, f)
				GenerateCastedFile(gen, gennedFile, f, allExtensions)
			}
		}
		gen.SupportedFeatures = gengo.SupportedFeatures
		return nil
	})
}
