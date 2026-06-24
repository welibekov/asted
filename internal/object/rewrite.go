package object

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// RewriteRequest defines where and what parameter to insert.
type RewriteRequest struct {
	Position  int
	Parameter string
	Default   string
}

func RewriteObject(foundObj *FoundObject, requests []RewriteRequest) error {
	if foundObj == nil || foundObj.Node == nil {
		return fmt.Errorf("foundObj or its AST node cannot be nil")
	}

	switch n := foundObj.Node.(type) {
	case *ast.FuncDecl:
		return rewriteFuncSignature(n.Type, requests)
	case *ast.Field:
		if ft, ok := n.Type.(*ast.FuncType); ok {
			return rewriteFuncSignature(ft, requests)
		}
		return fmt.Errorf("node is an ast.Field but its Type is not a function signature")
	default:
		return fmt.Errorf("unsupported node type: %T", foundObj.Node)
	}
}

func parseParameterStr(paramStr string) (*ast.Field, error) {
	dummySrc := fmt.Sprintf("package p; func _(%s) {}", paramStr)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", dummySrc, 0)
	if err != nil {
		return nil, fmt.Errorf("syntax error in parameter string: %w", err)
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
				return fn.Type.Params.List[0], nil
			}
		}
	}
	return nil, fmt.Errorf("failed to extract parsed AST field structural layout")
}
