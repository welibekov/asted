package object

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// RewriteImplementations finds all concrete struct methods that implement the target interface
// method and updates their signatures in-place. It returns a map of all modified files.
func RewriteImplementations(allPkgs []*packages.Package, foundObj *FoundObject, requests []RewriteRequest) (map[*ast.File]*packages.Package, error) {
	changedFiles := make(map[*ast.File]*packages.Package)

	if foundObj == nil || foundObj.Object == nil {
		return nil, fmt.Errorf("foundObj definition and type object cannot be nil")
	}

	// 1. Extract the target interface definition from the found object's receiver
	sig, ok := foundObj.Object.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return changedFiles, nil // Not a method (e.g. top-level function), no implementations exist
	}

	var targetInterface *types.Interface
	switch t := sig.Recv().Type().(type) {
	case *types.Interface:
		targetInterface = t
	case *types.Named:
		targetInterface, _ = t.Underlying().(*types.Interface)
	}

	// If the found object is already a concrete method rather than an interface,
	// there are no alternative structural interface implementations to cascade through.
	if targetInterface == nil {
		return changedFiles, nil
	}

	// 2. Scan every package in the workspace looking for concrete implementations
	for _, pkg := range allPkgs {
		if pkg.TypesInfo == nil {
			continue
		}

		for _, file := range pkg.Syntax {
			fileWasModified := false

			for _, decl := range file.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				// We only care about standalone functions that have a receiver block (methods)
				if !ok || funcDecl.Recv == nil {
					continue
				}

				// Resolve the method definition's type information
				funcObj := pkg.TypesInfo.Defs[funcDecl.Name]
				if funcObj == nil {
					continue
				}

				// Fast match: The method name must match the interface method name
				if funcObj.Name() != foundObj.Object.Name() {
					continue
				}

				funcSig := funcObj.Type().(*types.Signature)
				recvType := funcSig.Recv().Type()

				// Check if the receiver type (or its pointer variant) implements the target interface
				if types.Implements(recvType, targetInterface) || types.Implements(types.NewPointer(recvType), targetInterface) {
					// Mutate the concrete method signature in-place using our shared helper
					if err := rewriteFuncSignature(funcDecl.Type, requests); err != nil {
						return nil, err
					}
					fileWasModified = true
				}
			}

			if fileWasModified {
				changedFiles[file] = pkg
			}
		}
	}

	return changedFiles, nil
}

// rewriteFuncSignature is a shared utility that injects parameters into any *ast.FuncType in-place.
func rewriteFuncSignature(funcType *ast.FuncType, requests []RewriteRequest) error {
	if funcType == nil {
		return nil
	}
	if funcType.Params == nil {
		funcType.Params = &ast.FieldList{}
	}

	// Sort requests by Position in DESCENDING order to prevent index shifting
	sortedRequests := make([]RewriteRequest, len(requests))
	copy(sortedRequests, requests)
	sort.Slice(sortedRequests, func(i, j int) bool {
		return sortedRequests[i].Position > sortedRequests[j].Position
	})

	for _, req := range sortedRequests {
		newField, err := parseParameterStr(req.Parameter)
		if err != nil {
			return fmt.Errorf("failed to parse parameter '%s': %w", req.Parameter, err)
		}

		currentList := funcType.Params.List
		pos := req.Position

		if pos < 0 {
			pos = 0
		}
		if pos > len(currentList) {
			pos = len(currentList)
		}

		currentList = append(currentList, nil)
		copy(currentList[pos+1:], currentList[pos:])
		currentList[pos] = newField

		funcType.Params.List = currentList
	}

	// Clear positions to prevent multi-line disk layout corruption
	stripParamPositions(funcType.Params)
	return nil
}

// stripParamPositions sets container boundaries and inner type node targets to token.NoPos.
// This tells go/printer to ignore historical source code spacing and format parameters cleanly.
func stripParamPositions(params *ast.FieldList) {
	if params == nil {
		return
	}

	// 1. Wipe the tracking boundaries of the parent parameter parenthesis list
	params.Opening = token.NoPos
	params.Closing = token.NoPos

	for _, field := range params.List {
		if field == nil {
			continue
		}

		// 2. Clear identifier positions (parameter names like 'ctx', 'config')
		for _, name := range field.Names {
			if name != nil {
				name.NamePos = token.NoPos
			}
		}

		// 3. Recursively travel through the type tree to clear position remnants
		ast.Inspect(field.Type, func(n ast.Node) bool {
			if n == nil {
				return true
			}

			switch expr := n.(type) {
			case *ast.Ident:
				expr.NamePos = token.NoPos

			case *ast.SelectorExpr:
				// Handles package-qualified types like "context.Context"
				expr.Sel.NamePos = token.NoPos

			case *ast.StarExpr:
				// Handles pointer types like "*sync.Mutex"
				expr.Star = token.NoPos

			case *ast.ArrayType:
				// Handles slice or array types like "[]string"
				expr.Lbrack = token.NoPos

			case *ast.MapType:
				// Handles map structures like "map[string]int"
				expr.Map = token.NoPos

			case *ast.ChanType:
				// Handles channel types like "chan<- bool"
				expr.Begin = token.NoPos
				expr.Arrow = token.NoPos

			case *ast.Ellipsis:
				// Handles variadic parameters like "...interface{}"
				expr.Ellipsis = token.NoPos

			case *ast.InterfaceType:
				// Handles inline anonymous interfaces like "interface{ Read() }"
				expr.Interface = token.NoPos
				if expr.Methods != nil {
					expr.Methods.Opening = token.NoPos
					expr.Methods.Closing = token.NoPos
				}

			case *ast.StructType:
				// Handles inline anonymous structs like "struct{ ID string }"
				expr.Struct = token.NoPos
				if expr.Fields != nil {
					expr.Fields.Opening = token.NoPos
					expr.Fields.Closing = token.NoPos
				}

			case *ast.FuncType:
				// Handles higher-order callback functions passed as parameters
				expr.Func = token.NoPos
				if expr.Params != nil {
					expr.Params.Opening = token.NoPos
					expr.Params.Closing = token.NoPos
				}
				if expr.Results != nil {
					expr.Results.Opening = token.NoPos
					expr.Results.Closing = token.NoPos
				}
			}
			return true
		})
	}
}
