# Release Tool

A CLI tool for managing ECK (Elastic Cloud on Kubernetes) releases, built with Go, Cobra, and Viper.

## Overview

The release tool automates common release management tasks, particularly during feature freeze periods. It helps update version numbers across the codebase by replacing `-SNAPSHOT` versions with release versions.

## Building

To build the tool, you can use either the Makefile or go directly:

**Using Makefile:**
```bash
cd hack/release
make build
# Binary will be created at bin/release
```

**Using go directly:**
```bash
cd hack/release
go build -o release .
# Binary will be created as ./release
```

**Clean build artifacts:**
```bash
make clean
```

## Commands

### Primary Command

```
release [command]
```

The main entry point for all release operations.

### Sub-commands

#### `feature-freeze`

Feature freeze operations for managing release branches.

**Usage:**
```
release feature-freeze [command]
```

**Available Commands:**
- `update-stack-main-branch` - Update stack versions in main branch by incrementing minor version
- `update-stack-release-branch` - Update stack versions in release branch by removing -SNAPSHOT suffix

#### `feature-freeze update-stack-release-branch`

Updates stack versions in release branches by replacing `-SNAPSHOT` versions with release versions. This command:

- Scans `deploy/*` directory for version strings
- Scans `hack/operatorhub/*` directory for version strings
- Updates the `VERSION` file
- Optionally creates a new git branch for the changes

**Usage:**
```
release feature-freeze update-stack-release-branch [flags]
```

**Flags:**

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--release-version` | string | Yes | - | Release version (e.g., 3.3.0) |
| `--stack-version` | string | Yes | - | Elastic Stack version (e.g., 9.3.0) |
| `--create-branch` | bool | No | false | Create a local git branch |
| `--repo-path` | string | No | "." | Path to ECK repository (defaults to current directory) |

**Behavior:**

1. **Version Replacement Logic:**
   - Replaces versions matching the release version pattern (e.g., `3.3.0-SNAPSHOT` → `3.3.0`)
   - Replaces versions matching the stack version pattern (e.g., `9.3.0-SNAPSHOT` → `9.3.0`)
   - Uses major.minor matching to determine which version to apply

2. **Git Branch Validation:**
   - If `--create-branch` is set, the tool validates that the current git branch matches the major.minor part of the release version
   - For example, if `--release-version` is `3.3.0`, the current branch must be `3.3`
   - If validation fails, the command exits with an error

3. **File Processing:**
   - **deploy/** directory: Processes `.yaml`, `.yml`, `.tpl`, `.txt` files, and files without extensions
   - **hack/operatorhub/** directory: Processes `.yaml`, `.yml`, `.go`, `.tpl` files
   - **VERSION** file: Always processed

**Examples:**

```bash
# Update versions without creating a branch
./release feature-freeze update-stack-release-branch \
  --release-version 3.3.0 \
  --stack-version 9.3.0 \
  --repo-path /path/to/cloud-on-k8s

# Update versions and create a git branch
./release feature-freeze update-stack-release-branch \
  --release-version 3.3.0 \
  --stack-version 9.3.0 \
  --create-branch \
  --repo-path /path/to/cloud-on-k8s
```

**Environment Variables:**

All flags can also be set via environment variables with the `RELEASE_` prefix and underscores instead of hyphens:

- `RELEASE_RELEASE_VERSION` - Release version
- `RELEASE_STACK_VERSION` - Stack version
- `RELEASE_CREATE_BRANCH` - Create branch flag
- `RELEASE_REPO_PATH` - Repository path

#### `feature-freeze update-stack-main-branch`

Updates stack versions in the main branch by incrementing the minor version number. This command:

- Increments the release version by one minor version (e.g., `3.3.0` → `3.4.0`)
- Increments the stack version by one minor version (e.g., `9.3.0` → `9.4.0`)
- Scans `deploy/*` directory for version strings
- Scans `hack/operatorhub/*` directory for version strings
- Updates the `VERSION` file
- Optionally creates a new git branch for the changes

**Usage:**
```
release feature-freeze update-stack-main-branch [flags]
```

**Flags:**

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--release-version` | string | Yes | - | Current release version (e.g., 3.3.0). Will be incremented to 3.4.0 |
| `--stack-version` | string | Yes | - | Current Elastic Stack version (e.g., 9.3.0). Will be incremented to 9.4.0 |
| `--create-branch` | bool | No | false | Create a local git branch |
| `--repo-path` | string | No | "." | Path to ECK repository (defaults to current directory) |

**Behavior:**

1. **Version Increment Logic:**
   - Takes the provided `--release-version` and increments the minor version (e.g., `3.3.0` → `3.4.0`)
   - Takes the provided `--stack-version` and increments the minor version (e.g., `9.3.0` → `9.4.0`)
   - Replaces all occurrences of the old versions (with or without `-SNAPSHOT` suffix) with the new incremented versions

2. **Git Branch Validation:**
   - If `--create-branch` is set, the tool validates that the current git branch is `main`
   - If validation fails, the command exits with an error

3. **File Processing:**
   - **deploy/** directory: Processes `.yaml`, `.yml`, `.tpl`, `.txt` files, and files without extensions
   - **hack/operatorhub/** directory: Processes `.yaml`, `.yml`, `.go`, `.tpl` files
   - **VERSION** file: Always processed

**Examples:**

```bash
# Update versions without creating a branch
./release feature-freeze update-stack-main-branch \
  --release-version 3.3.0 \
  --stack-version 9.3.0 \
  --repo-path /path/to/cloud-on-k8s

# Update versions and create a git branch
./release feature-freeze update-stack-main-branch \
  --release-version 3.3.0 \
  --stack-version 9.3.0 \
  --create-branch \
  --repo-path /path/to/cloud-on-k8s
```

**Note:** The versions provided will be incremented by one minor version. For example, if you provide `--release-version 3.3.0`, it will be incremented to `3.4.0`.

## What It Does

### `update-stack-release-branch`

The `update-stack-release-branch` command performs the following operations:

1. **Validates Git Repository:** Ensures the specified path is a valid git repository
2. **Branch Validation (if creating branch):** Verifies the current branch matches the expected release branch
3. **Version Updates:** Scans and updates version strings in:
   - All files in `deploy/` directory
   - All files in `hack/operatorhub/` directory
   - The `VERSION` file in the repository root
4. **Git Branch Creation (optional):** Creates a new branch named `{release-version}-update-stack-release-branch`

This tool automates the process described in [PR #8988](https://github.com/elastic/cloud-on-k8s/pull/8988), which updates stack versions for release branches.

### `update-stack-main-branch`

The `update-stack-main-branch` command performs the following operations:

1. **Validates Git Repository:** Ensures the specified path is a valid git repository
2. **Branch Validation (if creating branch):** Verifies the current branch is `main`
3. **Version Incrementing:** Calculates new versions by incrementing minor version:
   - Release version: `3.3.0` → `3.4.0`
   - Stack version: `9.3.0` → `9.4.0`
4. **Version Updates:** Scans and replaces old versions with new incremented versions in:
   - All files in `deploy/` directory
   - All files in `hack/operatorhub/` directory
   - The `VERSION` file in the repository root
5. **Git Branch Creation (optional):** Creates a new branch named `{new-release-version}-update-stack-main-branch`
6. **API Documentation:** Runs `make generate-api-docs` to regenerate API documentation

This tool automates the process described in [PR #8985](https://github.com/elastic/cloud-on-k8s/pull/8985), which updates stack versions and helm versions in the main branch.

