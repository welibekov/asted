package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/welibekov/asted/internal/codebase"
	"github.com/welibekov/asted/internal/object"
)

// Global command flags for the mv command
var (
	filenameFlag string // Added to store the target filename flag value

	mvCmd = &cobra.Command{
		Use:   "mv [source-declaration] [destination-package]",
		Short: "Move and optionally rename a declaration across packages",
		Long: `Cuts a specified declaration (function, variable, constant, type) out of its source package,
grafts it into the destination package, and automatically repairs all call sites and import blocks across the entire workspace.

Examples:
  asted mv internal/auth/util.Ptr internal/compute
  asted mv internal/auth/util.Ptr internal/compute.NewPtr
  asted mv internal/auth/util.Ptr internal/compute --filename=engine.go`,
		Args: cobra.ExactArgs(2),
		RunE: executeMove,
	}
)

func init() {
	// Register the --filename / -f flag specifically to the 'mv' subcommand.
	mvCmd.Flags().StringVarP(&filenameFlag, "filename", "f", "", "Target file name for the moved declaration (e.g., engine.go)")
}

// executeMove handles the actual orchestration pipeline when 'asted mv' is invoked
func executeMove(cmd *cobra.Command, args []string) error {
	rawSrcInput := args[0] // e.g., "internal/auth/util.Ptr"
	rawDstInput := args[1] // e.g., "internal/compute"

	// SANITIZATION: If user passed '--filename=engine', automatically normalize it to 'engine.go'
	if filenameFlag != "" && !strings.HasSuffix(filenameFlag, ".go") {
		filenameFlag += ".go"
	}

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

	// 4. Parse and sanitize the inputs using our smart specification parser
	spec, err := codebase.ParseRefactorSpec(rawSrcInput, rawDstInput, modulePath)
	if err != nil {
		return fmt.Errorf("input spec parsing failure: %w", err)
	}

	// Handle methods and global symbols within this function
	if spec.IsMethod {
		cmd.Printf("[+] Analyzing contract graph and resolving implementations for method %s...\n", spec.SourceMethod)

		modifiedFiles, err := codebase.FixMethodRenames(pkgs, spec)
		if err != nil {
			return fmt.Errorf("method refactoring execution failure: %w", err)
		}

		cmd.Printf("[+] Flushing optimized modifications securely to disk storage...\n")
		if err := codebase.SaveModifiedFiles(modifiedFiles); err != nil {
			return fmt.Errorf("failed to flush refactored files back to storage: %w", err)
		}

		cmd.Printf("[✓] Successfully refactored method %s into %s across all implementations!\n", spec.SourceMethod, spec.DestMethod)
		return nil
	}

	// Global symbols processing
	foundObject, err := object.FindObject(pkgs, spec.SourcePkgPath, spec.SourceDecl)
	if err != nil {
		return fmt.Errorf("declaration lookup failure: %w", err)
	}

	cmd.Printf("[+] Rewriting call sites and managing imports across packages...\n")

	modifiedFiles, err := codebase.FixCallersAndImports(pkgs, foundObject, spec.DestPkgPath, spec.NewName)
	if err != nil {
		return fmt.Errorf("failed to modify cross-package callers: %w", err)
	}

	cmd.Printf("[+] Severing object from source and grafting into destination...\n")

	moveResult, err := object.MoveObject(pkgs, foundObject, spec.DestPkgPath, spec.NewName, filenameFlag)
	if err != nil {
		return fmt.Errorf("failed to process syntax tree transfer: %w", err)
	}

	// Register the freshly parsed file tree to the save queue map
	modifiedFiles[moveResult.SourceFile] = foundObject.Pkg
	modifiedFiles[moveResult.DestinationFile] = moveResult.DestinationPkg

	cmd.Printf("[+] Flushing optimized modifications securely to disk storage...\n")

	if err := codebase.SaveModifiedFiles(modifiedFiles); err != nil {
		return fmt.Errorf("failed to flush refactored files back to storage: %w", err)
	}

	cmd.Printf("[✓] Successfully refactored %s into %s!\n", spec.SourceDecl, spec.DestDecl)
	return nil
}
