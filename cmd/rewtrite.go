package cmd

import (
	"fmt"
	"go/ast"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/welibekov/asted/internal/codebase"
	"github.com/welibekov/asted/internal/object"
	"golang.org/x/tools/go/packages"
)

// Global command flags for the rewrite command
var (
	addParamFlag     string // Stores the parameter to add
	atFlag           int    // Position at which to add the parameter
	defaultValueFlag string // Stores the default value for the callers

	rewriteCmd = &cobra.Command{
		Use:   "rewrite [function-signature]",
		Short: "Rewrite a function signature by adding a new parameter",
		Long: `Allows the user to modify an existing function signature by adding a new parameter
at a specific position, along with a default value for the callers.

Examples:
  asted rewrite myFunction --add-param newParam --at 1 --default defaultValue
  asted rewrite myFunction --add-param newParam --at 2 --default 42`,
		Args: cobra.ExactArgs(1),
		RunE: executeRewrite,
	}
)

func init() {
	// Register the flags for the rewrite command
	rewriteCmd.Flags().StringVar(&addParamFlag, "add-param", "", "Parameter to add to the function signature")
	rewriteCmd.Flags().IntVar(&atFlag, "at", 0, "Position at which to add the parameter")
	rewriteCmd.Flags().StringVar(&defaultValueFlag, "default", "", "Default value for the callers")
}

// executeRewrite handles the logic for the rewrite command
func executeRewrite(cmd *cobra.Command, args []string) error {
	// 1. ATOMIC ENVIRONMENT SHIFT: Resolve and drop directly into the target directory context
	absProjectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for directory %q: %w", projectDir, err)
	}

	if err := os.Chdir(absProjectDir); err != nil {
		return fmt.Errorf("failed to switch process context to target directory %s: %w", absProjectDir, err)
	}

	cmd.Printf("[+] Scanning workspace analysis trees inside: %s\n", absProjectDir)
	cmd.Printf("[+] Scanning workspace analysis trees...\n")

	// 2. Configure the workspace loader
	pkgs, err := codebase.LoadPackages(absProjectDir)
	if err != nil {
		log.Fatal(err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found in the current working directory hierarchy")
	}

	// 3. Discover the active module path context dynamically
	var modulePath string
	for _, pkg := range pkgs {
		if pkg.Module != nil {
			modulePath = pkg.Module.Path // "github.com/welibekov/asted"
			break
		}
	}

	if modulePath == "" {
		return fmt.Errorf("no module path is found")
	}

	rawSrcInput := args[0] // e.g., "internal/auth/util.Ptr"
	rawDstInput := args[0]

	// 4. Parse and sanitize the inputs using our smart specification parser
	spec, err := codebase.ParseRefactorSpec(rawSrcInput, rawDstInput, modulePath)
	if err != nil {
		return fmt.Errorf("input spec parsing failure: %w", err)
	}

	if spec.IsMethod {
		spec.SourceDecl = fmt.Sprintf("%s.%s", spec.SourceTypeName, spec.SourceMethod)
	}

	// Global symbols processing
	obj, err := object.FindObject(pkgs, spec.SourcePkgPath, spec.SourceDecl)
	if err != nil {
		return fmt.Errorf("declaration lookup failure: %w", err)
	}

	request := []object.RewriteRequest{
		{
			Position:  atFlag,
			Parameter: addParamFlag,
			Default:   defaultValueFlag,
		},
	}

	if err := object.RewriteObject(obj, request); err != nil {
		return err
	}

	updatedImpls, err := object.RewriteImplementations(pkgs, obj, request)
	if err != nil {
		return err
	}

	updatedCallers, err := object.RewriteCallers(pkgs, obj, request)
	if err != nil {
		return err
	}
	allChangedFiles := make(map[*ast.File]*packages.Package)
	allChangedFiles[obj.File] = obj.Pkg

	for f, p := range updatedImpls {
		allChangedFiles[f] = p
	}
	for f, p := range updatedCallers {
		allChangedFiles[f] = p
	}

	if err := codebase.SaveModifiedFiles(allChangedFiles); err != nil {
		return fmt.Errorf("failed to flush refactored files back to storage: %w", err)
	}

	//for _, changes := range []map[*ast.File]*packages.Package{
	//	map[*ast.File]*packages.Package{
	//		obj.File: obj.Pkg,
	//	},
	//	updatedImpls,
	//	updatedCallers,
	//} {
	//	if err := codebase.SaveModifiedFiles(changes); err != nil {
	//		return fmt.Errorf("failed to flush refactored files back to storage: %w", err)
	//	}

	//}
	return nil
}
