package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	validateFile string
	validateDir  string
	validateJSON bool
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate desired-state manifests without mutating runtime state",
	Long: `Validate myassistant-style desired-state manifests in read-only mode.

Supported kinds today:
  - ManagedRepo
  - AgentOpsEcosystem
  - SelfHostingPolicy

Examples:
  agentctl validate -f ops/repos/book-assistant.yaml
  agentctl validate -R ops/`,
	Args: cobra.NoArgs,
	RunE: runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
	validateCmd.Flags().StringVarP(&validateFile, "file", "f", "", "Manifest file to validate")
	validateCmd.Flags().StringVarP(&validateDir, "recursive", "R", "", "Directory to scan recursively for YAML manifests")
	validateCmd.Flags().BoolVar(&validateJSON, "json", false, "Output machine-readable JSON")
}

func runValidate(cmd *cobra.Command, args []string) error {
	if (validateFile == "") == (validateDir == "") {
		return fmt.Errorf("exactly one of --file or --recursive must be specified")
	}

	var results []manifestValidationResult
	var err error
	if validateFile != "" {
		var result manifestValidationResult
		result, err = validateManifestFile(validateFile)
		if err != nil {
			return err
		}
		results = []manifestValidationResult{result}
	} else {
		results, err = validateManifestDir(validateDir)
		if err != nil {
			return err
		}
	}

	if validateJSON {
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding json: %w", err)
		}
		fmt.Println(string(out))
	} else {
		for _, result := range results {
			status := "OK"
			if !result.Valid {
				status = "INVALID"
			}
			label := result.Path
			if label == "" {
				label = result.Name
			}
			fmt.Printf("[%s] %s", status, label)
			if result.Kind != "" {
				fmt.Printf(" (%s", result.Kind)
				if result.Name != "" {
					fmt.Printf(" %s", result.Name)
				}
				fmt.Print(")")
			}
			fmt.Println()
			for _, msg := range result.Errors {
				fmt.Printf("  - %s\n", msg)
			}
		}
	}

	for _, result := range results {
		if !result.Valid {
			return fmt.Errorf("validation failed")
		}
	}
	return nil
}

func validateManifestFile(path string) (manifestValidationResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return manifestValidationResult{}, fmt.Errorf("read manifest %s: %w", path, err)
	}
	result := validateManifestContent(content)
	result.Path = path
	return result, nil
}

func validateManifestDir(root string) ([]manifestValidationResult, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan manifests under %s: %w", root, err)
	}
	sort.Strings(files)

	results := make([]manifestValidationResult, 0, len(files))
	for _, path := range files {
		result, err := validateManifestFile(path)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}
