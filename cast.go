package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"regexp"
	"sort"
	"strings"

	protobuf "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/iancoleman/strcase"
	"golang.org/x/tools/go/ast/astutil"
	"google.golang.org/protobuf/compiler/protogen"
)

// GenerateCastedFile generates a the cast typed contents of a .pb.go file.
func GenerateCastedFile(gen *protogen.Plugin, gennedFile *protogen.GeneratedFile, file *protogen.File, allExtensions []*protogen.Extension) {
	filename := file.GeneratedFilenamePrefix + ".pb.go"
	newGennedFile := gen.NewGeneratedFile(filename, file.GoImportPath)

	typeDefaultMap := map[string]string{
		"uint64": "0",
		"bytes": "nil",
	}

	fieldNameToOriginalType := make(map[string]string)
	fieldNameToCastType := make(map[string]string)
	fieldNameToStructTags := make(map[string]string)
	var newImports []string
	castify := func(castType string, field *protogen.Field) {
		if castType != "" {
			_, importedType := castTypeToGoType(castType)

			// Mark both keys in the case its modified in the resulting generation.
			key := fmt.Sprintf("%s", field.Desc.Name())
			log.Println(key)
			kind := field.Desc.Kind().String()
			camelKey := strcase.ToCamel(key)
			zeroValue := typeDefaultMap[kind]
			if field.Desc.IsList() {
				zeroValue = "nil"
				importedType = fmt.Sprintf("[]%s", importedType)
			}
			fieldNameToCastType[key] = importedType
			fieldNameToCastType[camelKey] = importedType
			fieldNameToCastType["Get" + field.GoName] = importedType

			fieldNameToOriginalType["Get" + field.GoName] = zeroValue
		}

		structTags, err := structTagsFromField(allExtensions, field)
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

	for _, message := range file.Messages {
		for _, field := range message.Fields {
			log.Printf("Field: %s\n", field.GoName)
			castType, err := castTypeFromField(allExtensions, field)
			if err != nil {
				panic(err)
			}
			importPath, _ := castTypeToGoType(castType)
			if importPath != "" {
				newImports = append(newImports, importPath)
			}
			castify(castType, field)
		}
		for _, mm := range message.Messages {
			log.Printf("Fields: %d\n", len(mm.Fields))
			for _, ffield := range mm.Fields {
				log.Printf("Field: %s\n", ffield.GoName)
				nestedCastType, err := castTypeFromField(allExtensions, ffield)
				if err != nil {
					panic(err)
				}
				castify(nestedCastType, ffield)
			}
		}
	}


	preFunc := func(c *astutil.Cursor) bool {
		return true
	}

	postFunc :=  func(c *astutil.Cursor) bool {
		n := c.Node()
		funcDecl, funcOk := n.(*ast.FuncDecl)
		if funcOk {
			funcName := funcDecl.Name.String()
			castType, castOk := fieldNameToCastType[funcName]
			if !castOk {
				return true
			}
			replacement := &ast.FuncDecl{
				Doc:  funcDecl.Doc,
				Recv: funcDecl.Recv,
				Name: funcDecl.Name,
				Type: funcDecl.Type,
				Body: funcDecl.Body,
			}
			body := replacement.Body.List
			if len(body) > 0 {
				lastStmt :=  body[len(body)-1]
				returnStmt, ok := lastStmt.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				castedReturn := ast.NewIdent(fmt.Sprintf("%s(%s)", castType, fieldNameToOriginalType[funcName]))
				returnStmt.Results[0] = castedReturn
				replacement.Body.List[len(body)-1] = returnStmt
			}
			replacement.Type.Results.List[0].Type = ast.NewIdent(castType)
			c.Replace(replacement)
			return true
		}
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

func castTypeFromField(allExtensions []*protogen.Extension, field *protogen.Field) (string, error) {
	var castTypeID uint64
	// Get the id for cast type extension.
	for _, ee := range allExtensions {
		if ee.Desc.Name() == "cast_type" {
			castTypeID = uint64(ee.Desc.Number())
		}
	}

	// Regex for it since names aren't easily visible.
	options := field.Desc.Options().(*protobuf.FieldOptions)
	regex, err := regexp.Compile(fmt.Sprintf("%d:\"([^\"]*)\"", castTypeID))
	if err != nil {
		return "", err
	}
	matches := regex.FindStringSubmatch(options.String())
	if len(matches) != 2 {
		return "", nil
	}
	return matches[1], nil
}

func structTagsFromField(extensions []*protogen.Extension, field *protogen.Field) (string, error) {
	idToName := make(map[uint64]string)
	for _, ee := range extensions {
		idToName[uint64(ee.Desc.Number())] = string(ee.Desc.Name())
	}

	var tags []string
	options := field.Desc.Options().(*protobuf.FieldOptions)
	for id, name := range idToName {
		regex, err := regexp.Compile(fmt.Sprintf("%d:\"([^\"]*)\"", id))
		if err != nil {
			return "", err
		}
		matches := regex.FindStringSubmatch(options.String())
		if len(matches) != 2 {
			continue
		}
		tags = append(tags, fmt.Sprintf(" %s:\"%s\"", snakeToCamel(name), matches[1]))
	}
	sort.Strings(tags)
	allTags := strings.Join(tags, "")
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
