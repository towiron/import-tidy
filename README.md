# Import-Tidy

A tool for automatically organizing and formatting import statements in Go files according to best practices.

## Overview

Import-Tidy scans Go files and ensures that import statements are properly grouped and ordered. It separates imports into three distinct groups:

1. **Standard Library** imports (e.g., "fmt", "os")
2. **External Library** imports (third-party packages)
3. **Internal Library** imports (your organization's internal packages)

The tool can either check for formatting issues or automatically fix them.

P.S. You can also add a custom order for import groups using the `--import-order` flag.

## Installation

```bash
go install github.com/towiron/import-tidy@latest
```

Or build from source:

```bash
git clone https://github.com/towiron/import-tidy.git
cd import-tidy
go build -o import-tidy
```

## Usage

```bash
import-tidy --internal-prefix=<your.internal.prefix> [--import-order=standard,external,internal] <path> [--fix]
```

### Parameters

- `--internal-prefix` (required): Specifies the import path prefix that identifies your organization's internal packages
- `--import-order` (optional): Define a custom order for import groups, using a comma-separated list (default: `standard,external,internal`)
- `<path>`: File or directory to process
- `--fix` (optional): Apply fixes automatically instead of just checking

### Examples

Check a single file:

```bash
import-tidy --internal-prefix=git.towiron.com main.go
```

Fix imports in a file:

```bash
import-tidy --internal-prefix=git.towiron.com main.go --fix
```

Process an entire directory and its subdirectories:

```bash
import-tidy --internal-prefix=git.towiron.com . --fix
```

Customize import order:

```bash
import-tidy --internal-prefix=git.towiron.com --import-order=external,standard,internal . --fix
```

## How It Works

Import-Tidy organizes imports by:

1. Identifying and categorizing imports into standard, external, and internal groups
2. Sorting imports alphabetically within each group
3. Adding appropriate spacing between groups
4. Removing unnecessary blank lines within groups
5. Enforcing a user-defined import order when specified

## Import Formatting Rules

The tool enforces the following rules:

- Imports are grouped by type (standard → external → internal, or as defined by `--import-order`)
- Each group is separated by a blank line
- No blank lines within a group
- Imports within each group are sorted alphabetically
- Import aliases are preserved
- Ensures consistent import order based on user-defined preferences

## Contributing

Contributions are welcome! If you find a bug or have a feature request, please open an issue or submit a pull request.

---

If you found this project helpful, I’d be grateful if you could give it a star! ⭐ If you spot any issues or have ideas for improvements, feel free to open a new issue. Your feedback is truly appreciated!

❤️ [Botir Shirmatov](https://github.com/towiron)
