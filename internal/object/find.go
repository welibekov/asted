package object

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// FindObject searches loaded packages for package items or structure methods.
func FindObject(pkgs []*packages.Package, pkgPath string, objName string) (*FoundObject, error) {
	// 1. Locate the target package first
	var targetPkg *packages.Package
	for _, pkg := range pkgs {
		if pkg.PkgPath == pkgPath {
			targetPkg = pkg
			break
		}
	}
	if targetPkg == nil {
		return nil, fmt.Errorf("package %s not found in loaded workspace", pkgPath)
	}

	var obj types.Object

	// 2. Resolve the Object (Handle Methods vs Package-level items)
	if strings.Contains(objName, ".") {
		// Method pattern: "StructName.MethodName"
		parts := strings.SplitN(objName, ".", 2)
		typeName, methodName := parts[0], parts[1]

		typeObj := targetPkg.Types.Scope().Lookup(typeName)
		if typeObj == nil {
			return nil, fmt.Errorf("base type %s not found in package %s", typeName, pkgPath)
		}

		// Dig into the named type's method list
		if named, ok := typeObj.Type().(*types.Named); ok {
			if iface, ok := named.Underlying().(*types.Interface); ok {
				for i := 0; i < iface.NumMethods(); i++ {
					m := iface.Method(i)
					if m.Name() == methodName {
						obj = m
						break
					}
				}
			}
		}
	} else {
		// Package-level pattern: functions, global variables, types, constants
		obj = targetPkg.Types.Scope().Lookup(objName)
	}

	if obj == nil {
		return nil, fmt.Errorf("object %s not found in package %s", objName, pkgPath)
	}

	// 3. Bridge to the AST layer using token.Pos
	pos := obj.Pos()
	var targetFile *ast.File
	var targetNode ast.Node

	for _, file := range targetPkg.Syntax {
		// Check if the object's position marker belongs inside this file's boundaries
		if pos >= file.Pos() && pos <= file.End() {
			targetFile = file

			// Walk the file's top-level declarations to match the exact position
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					return true
				}

				switch node := n.(type) {
				case *ast.FuncDecl: // Functions and methods
					if node.Name.Pos() == pos {
						targetNode = node
						return false
					}
				case *ast.TypeSpec: // Struct / Interface definitions
					if node.Name.Pos() == pos {
						targetNode = node
						return false
					}
				case *ast.ValueSpec: // Variables / Constants
					for _, ident := range node.Names {
						if ident.Pos() == pos {
							targetNode = node
							return false
						}
					}
				case *ast.Field:
					for _, ident := range node.Names { // Interface methods and struct fields
						if ident.Pos() == pos {
							targetNode = node
							return false
						}
					}
				}
				return true
			})
			break
		}
	}

	if targetFile == nil || targetNode == nil {
		return nil, fmt.Errorf("object %s found in type info, but AST nodes are missing", objName)
	}

	return &FoundObject{
		Object: obj,
		File:   targetFile,
		Node:   targetNode,
		Pkg:    targetPkg,
	}, nil
}
