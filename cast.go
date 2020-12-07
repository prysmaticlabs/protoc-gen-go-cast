package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"golang.org/x/tools/go/ast/astutil"
	"google.golang.org/protobuf/compiler/protogen"
)

// GenerateCastedFile generates a the cast typed contents of a .pb.go file.
func GenerateCastedFile(gen *protogen.Plugin, gennedFile *protogen.GeneratedFile, file *protogen.File) {
	filename := file.GeneratedFilenamePrefix + ".pb.go"
	newGennedFile := gen.NewGeneratedFile(filename, file.GoImportPath)

	var deps []*desc.FileDescriptor
	for path, fileDesc := range gen.FilesByPath {
		if *file.Proto.Name != path {
			fD, err := desc.CreateFileDescriptor(fileDesc.Proto)
			if err != nil {
				log.Fatalf("Could not create descriptor for %s: %v", fileDesc.Proto.Name, err)
			}
			deps = append(deps, fD)
		}
	}
	fileDesc, err := desc.CreateFileDescriptor(file.Proto, deps...)
	if err != nil {
		panic(err)
	}

	fieldNameToCastType := make(map[string]string)
	fieldNameToStructTags := make(map[string]string)
	var newImports []string
	for _, message := range file.Messages {
		for _, field := range message.Fields {
			castType, err := castTypeFromField(fileDesc, field)
			if err != nil {
				panic(err)
			}
			if castType != "" {
				importPath, importedType := castTypeToGoType(castType)
				if importPath != "" {
					newImports = append(newImports, importPath)
				}

				// Mark both keys in the case its modified in the resulting generation.
				key := fmt.Sprintf("%s", field.Desc.Name())
				camelKey := strcase.ToCamel(key)
				fieldNameToCastType[key] = importedType
				fieldNameToCastType[camelKey] = importedType
			}

			structTags, err := structTagsFromField(fileDesc, field)
			if err != nil {
				panic(err)
			}
			if structTags != "" {
				// Mark both keys in the case its modified in the resulting generation.
				key := fmt.Sprintf("%s", field.Desc.Name())
				camelKey := strcase.ToCamel(key)
				key = fmt.Sprintf("%s", field.Desc.Name())
				camelKey = strcase.ToCamel(key)
				fieldNameToStructTags[key] = structTags
				fieldNameToStructTags[camelKey] = structTags
			}
		}
	}


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
		replacement := &ast.Field{
			Doc: field.Doc,
			Names: field.Names,
			Type: field.Type,
			Tag: field.Tag,
			Comment: field.Comment,
		}
		name := field.Names[0].Name
		if castType, ok := fieldNameToCastType[name]; ok {
			replacement.Type = ast.NewIdent(castType)
		}
		if structTags, ok := fieldNameToStructTags[name]; ok {
			replacement.Tag = &ast.BasicLit{
				Kind: token.STRING,
				ValuePos: field.Tag.ValuePos,
				Value: fmt.Sprintf("%s%s`", field.Tag.Value[:len(field.Tag.Value)-1], structTags),
			}
		}
		c.Replace(replacement)
		return true
	}

	bytes, err := gennedFile.Content()
	if err != nil {
		panic(err)
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "",bytes, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	for _, importPath := range newImports {
		importName := namedImport(importPath)
		_ = astutil.AddNamedImport(fset, astFile, importName, importPath)
	}

	result := astutil.Apply(astFile, preFunc, postFunc)
	resultFile := result.(*ast.File)
	gennedFile.Skip()
	if err := printer.Fprint(newGennedFile, fset, resultFile); err != nil {
		panic(err)
	}
}

func castTypeFromField(fileDesc *desc.FileDescriptor, field *protogen.Field) (string, error) {
	registry := dynamic.NewExtensionRegistryWithDefaults()
	registry.AddExtensionsFromFile(fileDesc)
	optionReader := NewOptionReader(registry)

	// Find the message for given field.
	name := string(field.Desc.FullName())
	var fieldDesc *desc.FieldDescriptor
	for _, mm := range fileDesc.GetMessageTypes() {
		for _, ff := range mm.GetFields() {
			if ff.GetFullyQualifiedName() == name {
				fieldDesc = ff
			}
		}
	}
	if fieldDesc == nil {
		return "", fmt.Errorf("no field found for %s", name)
	}

	value, err := optionReader.GetOptionByName(fieldDesc, fmt.Sprintf("%s.cast_type", fileDesc.GetPackage()))
	if err == dynamic.ErrUnknownFieldName {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("could not read option by name: %v", err)
	}

	return value.(string), nil
}

func structTagsFromField(fileDesc *desc.FileDescriptor, field *protogen.Field) (string, error) {
	// Create the option registry.
	registry := dynamic.NewExtensionRegistryWithDefaults()
	registry.AddExtensionsFromFile(fileDesc)
	optionReader := NewOptionReader(registry)

	// Find the message for given field.
	name := string(field.Desc.FullName())
	var fieldDesc *desc.FieldDescriptor
	for _, mm := range fileDesc.GetMessageTypes() {
		for _, ff := range mm.GetFields() {
			if ff.GetFullyQualifiedName() == name {
				fieldDesc = ff
			}
		}
	}
	if fieldDesc == nil {
		return "", fmt.Errorf("no field found for %s", name)
	}

	// Find the extension and append its value. Only supports strings for simplicity.
	var allTags string
	for _, ext := range fileDesc.GetExtensions() {
		qualifiedName := ext.GetFullyQualifiedName()
		value, err := optionReader.GetOptionByName(fieldDesc, qualifiedName)
		if err != nil {
			return "", err
		}
		valueStr := value.(string)
		if valueStr == "" {
			continue
		}
		allTags += fmt.Sprintf(" %s:\"%s\"", snakeToCamel(ext.GetName()), valueStr)
	}
	return allTags, nil
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

func snakeToCamel(text string) string {
	newText := strings.ReplaceAll(text, "_", "-")
	return newText
}
