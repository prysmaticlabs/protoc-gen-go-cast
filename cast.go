package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// GenerateCastedFile generates a the cast typed contents of a .pb.go file.
func GenerateCastedFile(gen *protogen.Plugin, gennedFile *protogen.GeneratedFile, file *protogen.File, allExtensions []*protogen.Extension) {
	filename := file.GeneratedFilenamePrefix + ".pb.go"
	newGennedFile := gen.NewGeneratedFile(filename, file.GoImportPath)

	typeDefaultMap := map[string]string{
		"uint64": "0",
		"bytes":  "nil",
		"array":  "*new(([__size__]byte))", // __size__ is a placeholder for the actual size
	}

	fieldNameToOriginalType := make(map[string]string)
	fieldNameToCastType := make(map[string]string)
	fieldNameToStructTags := make(map[string]string)
	var newImports []string
	var kindName string
	castify := func(parentName string, key string, castType string, field *protogen.Field) {
		camelKey := toCamelInitCase(key, true)

		if castType != "" {
			var customTypeName string
			_, importedType := castTypeToGoType(castType)

			// Mark both keys in the case its modified in the resulting generation.
			kind := field.Desc.Kind()
			kindName = kind.String()

			// TODO: Extract to function and maybe write test
			if kind == protoreflect.BytesKind {
				fullTypeName, err := castTypeFromField(allExtensions, field)
				if err != nil {
					panic(err)
				}
				if strings.Contains(fullTypeName, "eth2-types"){
					kindName = "array"
					// We extract the name of the custom type without the package prefix.
					customTypeName =  fullTypeName[strings.LastIndex(fullTypeName, ".")+1:]
				}
			}

			zeroValue := typeDefaultMap[kindName]
			if kindName == "array" {
				switch customTypeName {
				case "Domain":
					{
						zeroValue = strings.Replace(zeroValue, "__size__", "32", 1)
					}
				}
			}

			if field.Desc.IsList() {
				zeroValue = "nil"
				importedType = fmt.Sprintf("[]%s", importedType)
			} else if field.Desc.HasOptionalKeyword() {
				importedType = fmt.Sprintf("*%s", importedType)
			}
			functionKey := fmt.Sprintf("%s-%s", parentName, "Get"+field.GoName)
			fieldNameToCastType[key] = importedType
			fieldNameToCastType[camelKey] = importedType
			fieldNameToCastType[functionKey] = importedType

			fieldNameToOriginalType[functionKey] = zeroValue
		}

		structTags, err := structTagsFromField(allExtensions, field)
		if err != nil {
			panic(err)
		}
		if structTags != "" {
			// Mark both keys in the case its modified in the resulting generation.
			fieldNameToStructTags[key] = structTags
			fieldNameToStructTags[camelKey] = structTags
		}
	}

	for _, message := range file.Messages {
		for _, field := range message.Fields {
			castType, err := castTypeFromField(allExtensions, field)
			if err != nil {
				panic(err)
			}
			importPath, _ := castTypeToGoType(castType)
			if importPath != "" {
				newImports = append(newImports, importPath)
			}
			key := fmt.Sprintf("%s-%s", field.Parent.Desc.Name(), field.GoName)
			receiverName := string(field.Parent.Desc.Name())
			castify(receiverName, key, castType, field)
		}
		for _, oneof := range message.Oneofs {
			for _, oneofField := range oneof.Fields {
				castType, err := castTypeFromField(allExtensions, oneofField)
				if err != nil {
					panic(err)
				}
				parentName := fmt.Sprintf("%s_%s", message.Desc.Name(), oneofField.GoName)
				key := fmt.Sprintf("%s-%s", parentName, oneofField.GoName)
				castify(parentName, key, castType, oneofField)
			}
		}
		for _, mm := range message.Messages {
			for _, ffield := range mm.Fields {
				nestedCastType, err := castTypeFromField(allExtensions, ffield)
				if err != nil {
					panic(err)
				}

				parentName := fmt.Sprintf("%s_%s", ffield.Parent.Desc.Parent().Name(), ffield.Parent.Desc.Name())
				key := fmt.Sprintf("%s-%s", parentName, ffield.GoName)
				castify(parentName, key, nestedCastType, ffield)
			}
			for _, mm := range message.Messages {
				for _, ffield := range mm.Fields {
					nestedCastType, err := castTypeFromField(allExtensions, ffield)
					if err != nil {
						panic(err)
					}
					parentName := fmt.Sprintf("%s_%s", ffield.Parent.Desc.Parent().Name(), ffield.Parent.Desc.Name())
					key := fmt.Sprintf("%s-%s", parentName, ffield.GoName)
					castify(parentName, key, nestedCastType, ffield)
				}
			}
		}
	}

	preFunc := func(c *astutil.Cursor) bool {
		return true
	}

	postFunc := func(c *astutil.Cursor) bool {
		n := c.Node()
		structType, structOk := n.(*ast.StructType)
		if structOk {
			replacementFields := structType.Fields
			decl, ok := c.Parent().(*ast.TypeSpec)
			if !ok {
				return true
			}
			for i, field := range replacementFields.List {
				if field.Tag == nil || len(field.Names) == 0 {
					continue
				}

				key := fmt.Sprintf("%s-%s", decl.Name, field.Names[0].Name)
				if castType, ok := fieldNameToCastType[key]; ok {
					replacementFields.List[i].Type = ast.NewIdent(castType)
				}
				if structTags, ok := fieldNameToStructTags[key]; ok {
					replacementFields.List[i].Tag = &ast.BasicLit{
						Kind:     token.STRING,
						ValuePos: field.Tag.ValuePos,
						Value:    fmt.Sprintf("%s%s`", field.Tag.Value[:len(field.Tag.Value)-1], structTags),
					}
				}
			}
			replacement := &ast.StructType{
				Struct:     structType.Struct,
				Fields:     replacementFields,
				Incomplete: structType.Incomplete,
			}
			c.Replace(replacement)
		}

		funcDecl, funcOk := n.(*ast.FuncDecl)
		if funcOk {
			funcName := funcDecl.Name.String()
			if !strings.Contains(funcName, "Get") {
				return true
			}

			var receiverType string
			if funcDecl.Recv != nil {
				receiver := funcDecl.Recv.List[0]
				x, ok := receiver.Type.(*ast.StarExpr)
				if ok {
					receiverType = fmt.Sprintf("%s", x.X)
				}
			}
			funcKey := fmt.Sprintf("%s-%s", receiverType, funcName)
			castType, castOk := fieldNameToCastType[funcKey]
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
			if len(replacement.Type.Params.List) > 0 {
				return true
			}
			body := replacement.Body.List
			if len(body) > 0 {
				lastStmt := body[len(body)-1]
				returnStmt, ok := lastStmt.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				newReturn := fmt.Sprintf("%s(%s)", castType, fieldNameToOriginalType[funcKey])
				castedReturn := ast.NewIdent(strings.Replace(newReturn, "*", "", -1))
				returnStmt.Results[0] = castedReturn
				replacement.Body.List[len(body)-1] = returnStmt
			}
			replacement.Type.Results.List[0].Type = ast.NewIdent(strings.Replace(castType, "*", "", -1))
			c.Replace(replacement)
			return true
		}
		return true
	}

	bytes, err := gennedFile.Content()
	if err != nil {
		panic(err)
	}
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "", bytes, parser.ParseComments)
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
	options := field.Desc.Options().(*descriptorpb.FieldOptions)
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
	options := field.Desc.Options().(*descriptorpb.FieldOptions)
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

// Converts a string to CamelCase
func toCamelInitCase(s string, initCase bool) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	n := strings.Builder{}
	n.Grow(len(s))
	capNext := initCase
	for i, v := range []byte(s) {
		vIsCap := v >= 'A' && v <= 'Z'
		vIsLow := v >= 'a' && v <= 'z'
		if capNext {
			if vIsLow {
				v += 'A'
				v -= 'a'
			}
		} else if i == 0 {
			if vIsCap {
				v += 'a'
				v -= 'A'
			}
		}
		if vIsCap || vIsLow {
			n.WriteByte(v)
			capNext = false
		} else if vIsNum := v >= '0' && v <= '9'; vIsNum {
			n.WriteByte(v)
			capNext = true
		} else {
			capNext = v == '_' || v == ' ' || v == '-' || v == '.'
		}
	}
	return n.String()
}
