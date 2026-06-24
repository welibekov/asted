package object

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// RewriteCallers finds all invocations of foundObj across the provided packages,
// injects the specified Default expressions into their arguments in-place, and
// returns a map of all files that were successfully mutated.
func RewriteCallers(allPkgs []*packages.Package, foundObj *FoundObject, requests []RewriteRequest) (map[*ast.File]*packages.Package, error) {
	// Initialize the tracking map for changed files
	changedFiles := make(map[*ast.File]*packages.Package)

	if foundObj == nil || foundObj.Object == nil {
		return nil, fmt.Errorf("foundObj definition and type object cannot be nil")
	}

	// 1. Sort requests by Position in DESCENDING order to prevent index shifting
	sortedRequests := make([]RewriteRequest, len(requests))
	copy(sortedRequests, requests)
	sort.Slice(sortedRequests, func(i, j int) bool {
		return sortedRequests[i].Position > sortedRequests[j].Position
	})

	// 2. Scan every package in the compilation workspace
	for _, pkg := range allPkgs {
		if pkg.TypesInfo == nil {
			continue // Skip packages without type-checker info
		}

		// Iterate through every parsed file AST in this package
		for _, file := range pkg.Syntax {
			var walkErr error
			fileWasModified := false

			// 3. Inspect the AST looking for Call Expressions
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil || walkErr != nil {
					return true
				}

				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				// 4. Resolve what object is actually being called
				calleeObj := getCalleeObject(pkg.TypesInfo, call.Fun)
				if !isMatchingCallee(calleeObj, foundObj.Object) {
					return true // Not our target method
				}

				// Mark that this file contains a mutation
				fileWasModified = true

				// 5. Found a caller! Inject default arguments in reverse order
				for _, req := range sortedRequests {
					newArg, err := parser.ParseExpr(req.Default)
					if err != nil {
						walkErr = fmt.Errorf("failed to parse default argument '%s': %w", req.Default, err)
						return false
					}

					// Clear layout positions on the new argument to prevent disk corruption
					stripExprPositions(newArg)

					pos := req.Position
					if pos < 0 {
						pos = 0
					}
					if pos > len(call.Args) {
						pos = len(call.Args)
					}

					// Standard in-place slice injection
					call.Args = append(call.Args, nil)
					copy(call.Args[pos+1:], call.Args[pos:])
					call.Args[pos] = newArg
				}

				// Clear parenthesis position metrics to force clean single-line gofmt spacing on disk
				call.Lparen = token.NoPos
				call.Rparen = token.NoPos

				return true
			})

			if walkErr != nil {
				return nil, walkErr
			}

			// 6. If the file was changed, track it alongside its parent package context
			if fileWasModified {
				changedFiles[file] = pkg
			}
		}
	}

	return changedFiles, nil
}

// getCalleeObject resolves an expression inside a call block to its definitions object.
func getCalleeObject(info *types.Info, fun ast.Expr) types.Object {
	switch expr := fun.(type) {
	case *ast.Ident:
		// Direct calls like `CreateVM(...)`
		return info.ObjectOf(expr)
	case *ast.SelectorExpr:
		// Method selections like `manager.CreateVM(...)`
		if sel, ok := info.Selections[expr]; ok {
			return sel.Obj()
		}
		// Package-qualified functions like `vm.CreateVM(...)`
		return info.ObjectOf(expr.Sel)
	default:
		return nil
	}
}

// stripExprPositions wipes token locations from a dynamically parsed expression.
func stripExprPositions(expr ast.Expr) {
	ast.Inspect(expr, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		switch node := n.(type) {
		case *ast.Ident:
			node.NamePos = token.NoPos
		case *ast.SelectorExpr:
			node.Sel.NamePos = token.NoPos
		case *ast.CallExpr:
			node.Lparen = token.NoPos
			node.Rparen = token.NoPos
		case *ast.BasicLit:
			node.ValuePos = token.NoPos
		}
		return true
	})
}

// isMatchingCallee checks if the invoked callee matches our target,
// resolving cross-boundary relationships between interface and concrete methods.
func isMatchingCallee(callee, target types.Object) bool {
	if callee == nil || target == nil {
		return false
	}
	// 1. Direct match (e.g., concrete-to-concrete or package functions)
	if callee == target {
		return true
	}

	// 2. Extract function characteristics
	calleeFunc, ok1 := callee.(*types.Func)
	targetFunc, ok2 := target.(*types.Func)
	if !ok1 || !ok2 {
		return false
	}

	// Names must be identical (e.g., "CreateVM")
	if calleeFunc.Name() != targetFunc.Name() {
		return false
	}

	calleeSig, ok1 := calleeFunc.Type().(*types.Signature)
	targetSig, ok2 := targetFunc.Type().(*types.Signature)
	if !ok1 || !ok2 || calleeSig.Recv() == nil || targetSig.Recv() == nil {
		return false // Not methods containing receivers
	}

	// 3. Determine which method is the interface and which is the concrete implementation
	var interfaceType *types.Interface
	var concreteRecv types.Type

	if it, ok := targetSig.Recv().Type().Underlying().(*types.Interface); ok {
		interfaceType = it
		concreteRecv = calleeSig.Recv().Type()
	} else if it, ok := calleeSig.Recv().Type().Underlying().(*types.Interface); ok {
		interfaceType = it
		concreteRecv = targetSig.Recv().Type()
	} else {
		// Both are concrete methods of different structs, so they don't match
		return false
	}

	// 4. If the concrete receiver type implements the interface, it's a match!
	if types.Implements(concreteRecv, interfaceType) || types.Implements(types.NewPointer(concreteRecv), interfaceType) {
		return true
	}

	return false
}
