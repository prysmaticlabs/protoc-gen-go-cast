package main

import (
	"testing"
)

func Test_castTypeToGoType(t *testing.T) {
	type returns struct {
		path string
		castType string
	}
	tests := []struct {
		name  string
		castType  string
		want  returns
	}{
		{
			name: "native type",
			castType:  "string",
			want: returns{
				path: 	"",
				castType: "string",
			},
		},
		{
			name: "url",
			castType:  "github.com/prysmaticlabs/go-bitfield.Bitfield",
			want: returns{
				path: 	"github.com/prysmaticlabs/go-bitfield",
				castType: "github_com_prysmaticlabs_go_bitfield.Bitfield",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, castType := castTypeToGoType(tt.castType)
			if path != tt.want.path {
				t.Errorf("castTypeToGoType() path = %v, want %v", path, tt.want.path)
			}
			if castType != tt.want.castType {
				t.Errorf("castTypeToGoType() castType = %v, want %v", castType, tt.want.castType)
			}
		})
	}
}


func Test_typeOverwritten(t *testing.T) {
	type returns struct {
		path string
		castType string
	}
	tests := []struct {
		name  string
		names	[]string
		castType  []string
		want  returns
	}{
		{
			name: "native type",
			castType:  "string",
			want: returns{
				path: 	"",
				castType: "string",
			},
		},
		{
			name: "url",
			castType:  "github.com/prysmaticlabs/go-bitfield.Bitfield",
			want: returns{
				path: 	"github.com/prysmaticlabs/go-bitfield",
				castType: "github_com_prysmaticlabs_go_bitfield.Bitfield",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, castType := castTypeToGoType(tt.castType)
			if path != tt.want.path {
				t.Errorf("castTypeToGoType() path = %v, want %v", path, tt.want.path)
			}
			if castType != tt.want.castType {
				t.Errorf("castTypeToGoType() castType = %v, want %v", castType, tt.want.castType)
			}
		})
	}
}

func Test_namedImport(t *testing.T) {
	tests := []struct {
		name string
		importPath string
		want string
	}{
		{
			name: "url",
			importPath:  "github.com/prysmaticlabs/go-bitfield",
			want: "github_com_prysmaticlabs_go_bitfield",
		},
		{
			name: "empty",
			importPath:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := namedImport(tt.importPath); got != tt.want {
				t.Errorf("namedImport() = %v, want %v", got, tt.want)
			}
		})
	}
}
