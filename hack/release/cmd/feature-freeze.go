// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	releaseVersion string
	stackVersion   string
	createBranch   bool
	repoPath       string
)

// NewFeatureFreezeCmd returns the feature-freeze command
func NewFeatureFreezeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feature-freeze",
		Short: "Feature freeze operations",
		Long:  `Commands for feature freeze operations, such as updating stack versions.`,
	}

	// Add common flags to parent command
	cmd.PersistentFlags().StringVar(&releaseVersion, "release-version", "", "Release version (e.g., 3.3.0)")
	cmd.PersistentFlags().StringVar(&stackVersion, "stack-version", "", "Elastic Stack version (e.g., 9.3.0)")
	cmd.PersistentFlags().BoolVar(&createBranch, "create-branch", false, "Create a local git branch")
	cmd.PersistentFlags().StringVar(&repoPath, "repo-path", ".", "Path to ECK repository (defaults to current directory)")

	viper.BindPFlag("release-version", cmd.PersistentFlags().Lookup("release-version"))
	viper.BindPFlag("stack-version", cmd.PersistentFlags().Lookup("stack-version"))
	viper.BindPFlag("create-branch", cmd.PersistentFlags().Lookup("create-branch"))
	viper.BindPFlag("repo-path", cmd.PersistentFlags().Lookup("repo-path"))

	cmd.AddCommand(NewUpdateStackReleaseBranchCmd())
	cmd.AddCommand(NewUpdateStackMainBranchCmd())

	return cmd
}

// NewUpdateStackReleaseBranchCmd returns the update-stack-release-branch subcommand
func NewUpdateStackReleaseBranchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-stack-release-branch",
		Short: "Update stack versions in release branch by removing -SNAPSHOT suffix",
		Long: `Update stack versions in release branch in deploy/*, hack/operatorhub/*, and VERSION file
by replacing -SNAPSHOT versions with release versions.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Bind flags to viper
			if err := viper.BindPFlags(cmd.PersistentFlags()); err != nil {
				return fmt.Errorf("failed to bind persistent flags: %w", err)
			}

			// Set values from viper
			releaseVersion = viper.GetString("release-version")
			stackVersion = viper.GetString("stack-version")
			createBranch = viper.GetBool("create-branch")
			repoPath = viper.GetString("repo-path")

			// Validate required flags
			if releaseVersion == "" {
				return fmt.Errorf("--release-version is required")
			}
			if stackVersion == "" {
				return fmt.Errorf("--stack-version is required")
			}

			return nil
		},
		RunE: runUpdateStackReleaseBranch,
	}

	return cmd
}

// NewUpdateStackMainBranchCmd returns the update-stack-main-branch subcommand
func NewUpdateStackMainBranchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-stack-main-branch",
		Short: "Update stack versions in main branch by incrementing minor version",
		Long: `Update stack versions in main branch in deploy/*, hack/operatorhub/*, and VERSION file
by incrementing the minor version number (e.g., 3.3.0 -> 3.4.0, 9.3.0 -> 9.4.0).`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Bind flags to viper
			if err := viper.BindPFlags(cmd.PersistentFlags()); err != nil {
				return fmt.Errorf("failed to bind persistent flags: %w", err)
			}

			// Set values from viper
			releaseVersion = viper.GetString("release-version")
			stackVersion = viper.GetString("stack-version")
			createBranch = viper.GetBool("create-branch")
			repoPath = viper.GetString("repo-path")

			// Validate required flags
			if releaseVersion == "" {
				return fmt.Errorf("--release-version is required")
			}
			if stackVersion == "" {
				return fmt.Errorf("--stack-version is required")
			}

			return nil
		},
		RunE: runUpdateStackMainBranch,
	}

	return cmd
}

func runUpdateStackReleaseBranch(cmd *cobra.Command, args []string) error {
	// Resolve absolute path
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("failed to resolve repository path: %w", err)
	}

	// Create git branch if requested
	if createBranch {
		// Validate that we're on the correct branch
		currentBranch, err := getCurrentGitBranch(absPath)
		if err != nil {
			return fmt.Errorf("failed to get current git branch: %w", err)
		}

		// Check if current branch matches the major.minor part of the release version
		// (e.g., if release version is "3.3.0", only accept branch "3.3")
		expectedBranch := getMajorMinor(releaseVersion)

		if currentBranch != expectedBranch {
			return fmt.Errorf("current branch '%s' does not match expected branch '%s'. Please checkout branch '%s' before running this command", currentBranch, expectedBranch, expectedBranch)
		}

		branchName := fmt.Sprintf("%s-update-stack-release-branch", releaseVersion)
		if err := createGitBranch(absPath, branchName); err != nil {
			return fmt.Errorf("failed to create git branch: %w", err)
		}
		fmt.Printf("Created git branch: %s\n", branchName)
	}

	// Update files in deploy/* directory
	if err := updateDeployDirectory(absPath, releaseVersion, stackVersion); err != nil {
		return fmt.Errorf("failed to update deploy directory: %w", err)
	}

	// Update files in hack/operatorhub/* directory
	if err := updateOperatorHubDirectory(absPath, releaseVersion, stackVersion); err != nil {
		return fmt.Errorf("failed to update hack/operatorhub directory: %w", err)
	}

	// Update VERSION file
	if err := updateVersionFile(absPath, releaseVersion); err != nil {
		return fmt.Errorf("failed to update VERSION file: %w", err)
	}

	// Run make generate-api-docs
	if err := runMakeGenerateAPIDocs(absPath); err != nil {
		return fmt.Errorf("failed to run make generate-api-docs: %w", err)
	}

	fmt.Println("Successfully updated all stack versions in release branch")
	fmt.Println("\nEnsure you add the newly created files in docs/reference/* to your commit")
	return nil
}

func validateGitRepository(repoPath string) error {
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("not a git repository: %s", repoPath)
	}
	return nil
}

func runGitCommand(repoPath string, args []string, errorMsg string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\nOutput: %s", errorMsg, err, string(output))
	}
	return output, nil
}

func getCurrentGitBranch(repoPath string) (string, error) {
	if err := validateGitRepository(repoPath); err != nil {
		return "", err
	}

	output, err := runGitCommand(repoPath, []string{"branch", "--show-current"}, "failed to get current git branch")
	if err != nil {
		return "", err
	}

	branch := strings.TrimSpace(string(output))
	return branch, nil
}

func createGitBranch(repoPath, branchName string) error {
	if err := validateGitRepository(repoPath); err != nil {
		return err
	}

	_, err := runGitCommand(repoPath, []string{"checkout", "-b", branchName}, fmt.Sprintf("failed to create git branch %s", branchName))
	return err
}

func runMakeGenerateAPIDocs(repoPath string) error {
	cmd := exec.Command("make", "generate-api-docs")
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run make generate-api-docs: %w\nOutput: %s", err, string(output))
	}
	fmt.Println("Generated API documentation")
	return nil
}

func updateDirectory(repoPath, dirPath, releaseVersion, stackVersion string, shouldProcessFile func(string) bool) error {
	fullPath := filepath.Join(repoPath, dirPath)
	return filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Process files based on the provided function
		if shouldProcessFile(path) {
			return updateFileVersions(path, releaseVersion, stackVersion)
		}

		return nil
	})
}

func updateDeployDirectory(repoPath, releaseVersion, stackVersion string) error {
	shouldProcess := func(path string) bool {
		return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") ||
			strings.HasSuffix(path, ".tpl") || strings.HasSuffix(path, ".txt") ||
			!strings.Contains(path, ".")
	}
	return updateDirectory(repoPath, "deploy", releaseVersion, stackVersion, shouldProcess)
}

func updateOperatorHubDirectory(repoPath, releaseVersion, stackVersion string) error {
	shouldProcess := func(path string) bool {
		return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") ||
			strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".tpl")
	}
	return updateDirectory(repoPath, "hack/operatorhub", releaseVersion, stackVersion, shouldProcess)
}

func updateVersionFile(repoPath, releaseVersion string) error {
	versionPath := filepath.Join(repoPath, "VERSION")
	return updateFileVersions(versionPath, releaseVersion, "")
}

func updateFileVersions(filePath, releaseVersion, stackVersion string) error {
	// Get file info to preserve permissions
	info, err := os.Stat(filePath)
	if err != nil {
		// File might not exist, skip it
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	// Read file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	originalContent := string(content)
	updatedContent := originalContent

	// Replace all versions ending with -SNAPSHOT
	// Use a heuristic: if the version before -SNAPSHOT matches the major.minor pattern of releaseVersion,
	// replace with releaseVersion. Otherwise, if it matches stackVersion pattern, replace with stackVersion.
	versionRegex := regexp.MustCompile(`(\d+\.\d+\.\d+)-SNAPSHOT`)
	updatedContent = versionRegex.ReplaceAllStringFunc(updatedContent, func(match string) string {
		versionWithoutSnapshot := strings.TrimSuffix(match, "-SNAPSHOT")
		// Check if this looks like an ECK version (starts with same major.minor as releaseVersion)
		if strings.HasPrefix(versionWithoutSnapshot, getMajorMinor(releaseVersion)) {
			return releaseVersion
		}
		// Check if this looks like a stack version (starts with same major.minor as stackVersion)
		if stackVersion != "" && strings.HasPrefix(versionWithoutSnapshot, getMajorMinor(stackVersion)) {
			return stackVersion
		}
		// Default: remove -SNAPSHOT
		return versionWithoutSnapshot
	})

	// Only write if content changed
	if updatedContent != originalContent {
		if err := os.WriteFile(filePath, []byte(updatedContent), info.Mode()); err != nil {
			return fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fmt.Printf("Updated: %s\n", filePath)
	}

	return nil
}

func getMajorMinor(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

func incrementMinorVersion(version string) (string, error) {
	v, err := semver.Parse(version)
	if err != nil {
		return "", fmt.Errorf("failed to parse version %s: %w", version, err)
	}
	v.Minor++
	v.Patch = 0
	return v.String(), nil
}

func runUpdateStackMainBranch(cmd *cobra.Command, args []string) error {
	// Resolve absolute path
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("failed to resolve repository path: %w", err)
	}

	// Increment versions
	newReleaseVersion, err := incrementMinorVersion(releaseVersion)
	if err != nil {
		return fmt.Errorf("failed to increment release version: %w", err)
	}

	newStackVersion, err := incrementMinorVersion(stackVersion)
	if err != nil {
		return fmt.Errorf("failed to increment stack version: %w", err)
	}

	fmt.Printf("Incrementing versions:\n")
	fmt.Printf("  Release: %s -> %s\n", releaseVersion, newReleaseVersion)
	fmt.Printf("  Stack: %s -> %s\n", stackVersion, newStackVersion)

	// Create git branch if requested
	if createBranch {
		// Validate that we're on the main branch
		currentBranch, err := getCurrentGitBranch(absPath)
		if err != nil {
			return fmt.Errorf("failed to get current git branch: %w", err)
		}

		if currentBranch != "main" {
			return fmt.Errorf("current branch '%s' is not 'main'. Please checkout 'main' branch before running this command", currentBranch)
		}

		branchName := fmt.Sprintf("%s-update-stack-main-branch", newReleaseVersion)
		if err := createGitBranch(absPath, branchName); err != nil {
			return fmt.Errorf("failed to create git branch: %w", err)
		}
		fmt.Printf("Created git branch: %s\n", branchName)
	}

	// Update files in deploy/* directory
	if err := updateDeployDirectoryIncrement(absPath, releaseVersion, newReleaseVersion, stackVersion, newStackVersion); err != nil {
		return fmt.Errorf("failed to update deploy directory: %w", err)
	}

	// Update files in hack/operatorhub/* directory
	if err := updateOperatorHubDirectoryIncrement(absPath, releaseVersion, newReleaseVersion, stackVersion, newStackVersion); err != nil {
		return fmt.Errorf("failed to update hack/operatorhub directory: %w", err)
	}

	// Update VERSION file
	if err := updateVersionFileIncrement(absPath, releaseVersion, newReleaseVersion); err != nil {
		return fmt.Errorf("failed to update VERSION file: %w", err)
	}

	// Run make generate-api-docs
	if err := runMakeGenerateAPIDocs(absPath); err != nil {
		return fmt.Errorf("failed to run make generate-api-docs: %w", err)
	}

	fmt.Println("Successfully updated all stack versions in main branch")
	fmt.Println("\nEnsure you add the newly created files in docs/reference/* to your commit")
	return nil
}

func updateDirectoryIncrement(repoPath, dirPath, oldReleaseVersion, newReleaseVersion, oldStackVersion, newStackVersion string, shouldProcessFile func(string) bool) error {
	fullPath := filepath.Join(repoPath, dirPath)
	return filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Process files based on the provided function
		if shouldProcessFile(path) {
			return updateFileVersionsIncrement(path, oldReleaseVersion, newReleaseVersion, oldStackVersion, newStackVersion)
		}

		return nil
	})
}

func updateDeployDirectoryIncrement(repoPath, oldReleaseVersion, newReleaseVersion, oldStackVersion, newStackVersion string) error {
	shouldProcess := func(path string) bool {
		return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") ||
			strings.HasSuffix(path, ".tpl") || strings.HasSuffix(path, ".txt") ||
			!strings.Contains(path, ".")
	}
	return updateDirectoryIncrement(repoPath, "deploy", oldReleaseVersion, newReleaseVersion, oldStackVersion, newStackVersion, shouldProcess)
}

func updateOperatorHubDirectoryIncrement(repoPath, oldReleaseVersion, newReleaseVersion, oldStackVersion, newStackVersion string) error {
	shouldProcess := func(path string) bool {
		return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") ||
			strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".tpl")
	}
	return updateDirectoryIncrement(repoPath, "hack/operatorhub", oldReleaseVersion, newReleaseVersion, oldStackVersion, newStackVersion, shouldProcess)
}

func updateVersionFileIncrement(repoPath, oldReleaseVersion, newReleaseVersion string) error {
	versionPath := filepath.Join(repoPath, "VERSION")
	return updateFileVersionsIncrement(versionPath, oldReleaseVersion, newReleaseVersion, "", "")
}

func updateFileVersionsIncrement(filePath, oldReleaseVersion, newReleaseVersion, oldStackVersion, newStackVersion string) error {
	// Get file info to preserve permissions
	info, err := os.Stat(filePath)
	if err != nil {
		// File might not exist, skip it
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	// Read file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	originalContent := string(content)
	updatedContent := originalContent

	// Replace old release version with new release version
	// Match versions with or without -SNAPSHOT suffix
	releaseVersionPattern := regexp.MustCompile(regexp.QuoteMeta(oldReleaseVersion))
	updatedContent = releaseVersionPattern.ReplaceAllString(updatedContent, newReleaseVersion)

	// Replace old stack version with new stack version (if provided)
	if oldStackVersion != "" && newStackVersion != "" {
		stackVersionPattern := regexp.MustCompile(regexp.QuoteMeta(oldStackVersion))
		updatedContent = stackVersionPattern.ReplaceAllString(updatedContent, newStackVersion)
	}

	// Also handle -SNAPSHOT versions by replacing old version-SNAPSHOT with new version-SNAPSHOT
	releaseSnapshotPattern := regexp.MustCompile(regexp.QuoteMeta(oldReleaseVersion + "-SNAPSHOT"))
	updatedContent = releaseSnapshotPattern.ReplaceAllString(updatedContent, newReleaseVersion+"-SNAPSHOT")

	if oldStackVersion != "" && newStackVersion != "" {
		stackSnapshotPattern := regexp.MustCompile(regexp.QuoteMeta(oldStackVersion + "-SNAPSHOT"))
		updatedContent = stackSnapshotPattern.ReplaceAllString(updatedContent, newStackVersion+"-SNAPSHOT")
	}

	// Only write if content changed
	if updatedContent != originalContent {
		if err := os.WriteFile(filePath, []byte(updatedContent), info.Mode()); err != nil {
			return fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fmt.Printf("Updated: %s\n", filePath)
	}

	return nil
}
