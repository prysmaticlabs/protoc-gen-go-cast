package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// GenerateCastedFile generates a the cast typed contents of a .pb.go file.
func GenerateCastedFile(gen *protogen.Plugin, file *protogen.File) {
	filename := "test/" + file.GeneratedFilenamePrefix + ".pb.go"

	fieldNameToCastType := make(map[string]string)
	var newImports []string
	for _, message := range file.Messages {
		for _, field := range message.Fields {
			castType := castTypeFromField(field)
			if castType == "" {
				continue
			}
			importPath, importedType := castTypeToGoType(castType)
			if importPath != "" {
				newImports = append(newImports, importPath)
			}
			key := fmt.Sprintf("%s", field.Desc.Name())
			fieldNameToCastType[key] = importedType
		}
	}


	fset := token.NewFileSet()
	preFunc := func(c *astutil.Cursor) bool {
		n := c.Node()
		switch n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.ArrayType:
			return false
		case *ast.CommentGroup:
			return false
		case *ast.Comment:
			return false
		}

		return true
	}

	postFunc :=  func(c *astutil.Cursor) bool {
		n := c.Node()
		field, ok := n.(*ast.Field)
		if !ok {
			return true
		}
		if len(field.Names)  == 0 {
			return true
		}
		name := field.Names[0].Name
		if castType, ok := fieldNameToCastType[name]; ok {
			replacement := &ast.Field{
				Doc: field.Doc,
				Names: field.Names,
				Type: ast.NewIdent(castType),
				Tag: field.Tag,
				Comment: field.Comment,
			}
			c.Replace(replacement)
		}
		return true
	}

	astFile, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	for _, importPath := range newImports {
		importName := namedImport(importPath)
		_ = astutil.AddNamedImport(fset, astFile, importName, importPath)
	}

	result := astutil.Apply(astFile, preFunc, postFunc)
	resultfile := result.(*ast.File)
	if err := os.Remove(filename); err != nil {
		panic(err)
	}
	writer, err := os.Create("genned.go")
	if err != nil {
		panic(err)
	}
	defer writer.Close()
	if err := printer.Fprint(writer, fset, resultfile); err != nil {
		panic(err)
	}
}

func castTypeFromField(field *protogen.Field) string {
	options := field.Desc.Options().(*descriptorpb.FieldOptions)
	result := proto.GetExtension(options, E_CastTypeOption).(string)
	return result
}

func castTypeToGoType(castType string) (string, string) {
	typeStartIdx := strings.LastIndex(castType, ".")
	if typeStartIdx == -1 {
		return "", castType
	}
	importPath := castType[:typeStartIdx]
	importedType := castType[typeStartIdx+1:]
	return importPath, fmt.Sprintf("%s.%s", namedImport(importPath), importedType)
}

func namedImport(importPath string) string {
	importName := strings.ReplaceAll(importPath, "/", "_")
	importName = strings.ReplaceAll(importName, "-", "_")
	importName = strings.ReplaceAll(importName, ".", "_")
	return importName
}

