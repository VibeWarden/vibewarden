package dockerfile

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Lang identifies a programming language toolchain.
type Lang string

const (
	// LangGo is the Go programming language.
	LangGo Lang = "go"
	// LangNode is the Node.js runtime.
	LangNode Lang = "node"
	// LangPython is the Python runtime.
	LangPython Lang = "python"
	// LangNone means no recognised toolchain manifest was found.
	LangNone Lang = ""
)

// Toolchain holds the detected language and the required major.minor version
// extracted from the project's toolchain manifest.
type Toolchain struct {
	// Lang is the identified language. LangNone when detection failed.
	Lang Lang
	// Major is the major version component (e.g. 1 for Go 1.26).
	Major int
	// Minor is the minor version component (e.g. 26 for Go 1.26).
	Minor int
	// Source is a human-readable description of the manifest file and field
	// from which the version was extracted (e.g. "go.mod", ".nvmrc").
	Source string
}

// DetectToolchain scans projectRoot for known toolchain manifests and returns
// the first one it finds. The detection order is: Go (go.mod) → Node (.nvmrc,
// package.json) → Python (pyproject.toml, .python-version). When no manifest
// is found, it returns (Toolchain{}, false, nil). A read error on a detected
// file returns (Toolchain{}, false, err).
func DetectToolchain(projectRoot string) (Toolchain, bool, error) {
	// Go.
	if tc, ok, err := detectGo(projectRoot); err != nil || ok {
		return tc, ok, err
	}
	// Node.
	if tc, ok, err := detectNode(projectRoot); err != nil || ok {
		return tc, ok, err
	}
	// Python.
	if tc, ok, err := detectPython(projectRoot); err != nil || ok {
		return tc, ok, err
	}
	return Toolchain{}, false, nil
}

// RequiredToolchainVersion returns the required major.minor version string for
// the project (e.g. "1.26" for Go, "20.11" for Node). It is a convenience
// wrapper around DetectToolchain. Returns ("", false, nil) when no manifest is
// detected.
func RequiredToolchainVersion(projectRoot string, lang Lang) (string, bool, error) {
	tc, ok, err := DetectToolchain(projectRoot)
	if !ok || err != nil {
		return "", ok, err
	}
	if tc.Lang != lang {
		return "", false, nil
	}
	return fmt.Sprintf("%d.%d", tc.Major, tc.Minor), true, nil
}

// ─── Go ──────────────────────────────────────────────────────────────────────

var reGoDirective = regexp.MustCompile(`(?m)^go (\d+)\.(\d+)(?:\.\d+)?`)

func detectGo(root string) (Toolchain, bool, error) {
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path) //nolint:gosec // root is the project root
	if os.IsNotExist(err) {
		return Toolchain{}, false, nil
	}
	if err != nil {
		return Toolchain{}, false, fmt.Errorf("reading go.mod: %w", err)
	}
	m := reGoDirective.FindSubmatch(data)
	if m == nil {
		return Toolchain{}, false, nil
	}
	major, _ := strconv.Atoi(string(m[1]))
	minor, _ := strconv.Atoi(string(m[2]))
	return Toolchain{Lang: LangGo, Major: major, Minor: minor, Source: "go.mod"}, true, nil
}

// ─── Node ─────────────────────────────────────────────────────────────────────

var reVersion = regexp.MustCompile(`(\d+)(?:\.(\d+))?`)

func detectNode(root string) (Toolchain, bool, error) {
	// Prefer .nvmrc.
	nvmrcPath := filepath.Join(root, ".nvmrc")
	if data, err := os.ReadFile(nvmrcPath); err == nil { //nolint:gosec
		version := strings.TrimSpace(string(data))
		// Skip lts/* aliases — format unrecognised.
		if strings.HasPrefix(strings.ToLower(version), "lts/") {
			return Toolchain{}, false, nil
		}
		// Strip leading "v".
		version = strings.TrimPrefix(version, "v")
		m := reVersion.FindStringSubmatch(version)
		if m != nil {
			major, _ := strconv.Atoi(m[1])
			minor := 0
			if m[2] != "" {
				minor, _ = strconv.Atoi(m[2])
			}
			return Toolchain{Lang: LangNode, Major: major, Minor: minor, Source: ".nvmrc"}, true, nil
		}
		return Toolchain{}, false, nil
	} else if !os.IsNotExist(err) {
		return Toolchain{}, false, fmt.Errorf("reading .nvmrc: %w", err)
	}

	// Fall back to package.json engines.node.
	pkgPath := filepath.Join(root, "package.json")
	data, err := os.ReadFile(pkgPath) //nolint:gosec
	if os.IsNotExist(err) {
		return Toolchain{}, false, nil
	}
	if err != nil {
		return Toolchain{}, false, fmt.Errorf("reading package.json: %w", err)
	}
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return Toolchain{}, false, nil // malformed JSON — skip
	}
	if pkg.Engines.Node == "" {
		return Toolchain{}, false, nil
	}
	// Extract the lower bound from range expressions like ">=20", ">=20.11",
	// ">=20.11.0 <22". Pick the first integer sequence.
	nodeVer := pkg.Engines.Node
	// Strip leading comparison operators.
	nodeVer = strings.TrimLeft(nodeVer, "><=~^v ")
	// Take up to the first space or comma (handles range upper bound).
	if idx := strings.IndexAny(nodeVer, " ,"); idx >= 0 {
		nodeVer = nodeVer[:idx]
	}
	m := reVersion.FindStringSubmatch(nodeVer)
	if m == nil {
		return Toolchain{}, false, nil
	}
	major, _ := strconv.Atoi(m[1])
	minor := 0
	if m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	return Toolchain{Lang: LangNode, Major: major, Minor: minor, Source: "package.json#engines.node"}, true, nil
}

// ─── Python ───────────────────────────────────────────────────────────────────

var rePythonVersion = regexp.MustCompile(`(\d+)\.(\d+)`)
var rePyprojectRequires = regexp.MustCompile(`(?m)^requires-python\s*=\s*["']([^"']+)["']`)

func detectPython(root string) (Toolchain, bool, error) {
	// Prefer pyproject.toml.
	pyprojectPath := filepath.Join(root, "pyproject.toml")
	if data, err := os.ReadFile(pyprojectPath); err == nil { //nolint:gosec
		m := rePyprojectRequires.FindSubmatch(data)
		if m != nil {
			ver := parsePythonVersionConstraint(string(m[1]))
			if vm := rePythonVersion.FindStringSubmatch(ver); vm != nil {
				major, _ := strconv.Atoi(vm[1])
				minor, _ := strconv.Atoi(vm[2])
				return Toolchain{Lang: LangPython, Major: major, Minor: minor, Source: "pyproject.toml#requires-python"}, true, nil
			}
		}
		return Toolchain{}, false, nil
	} else if !os.IsNotExist(err) {
		return Toolchain{}, false, fmt.Errorf("reading pyproject.toml: %w", err)
	}

	// Fall back to .python-version (single line: "3.11.4" or "3.11").
	pvPath := filepath.Join(root, ".python-version")
	if data, err := os.ReadFile(pvPath); err == nil { //nolint:gosec
		line := strings.TrimSpace(string(data))
		// Read the first non-empty line.
		if idx := strings.IndexByte(line, '\n'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if m := rePythonVersion.FindStringSubmatch(line); m != nil {
			major, _ := strconv.Atoi(m[1])
			minor, _ := strconv.Atoi(m[2])
			return Toolchain{Lang: LangPython, Major: major, Minor: minor, Source: ".python-version"}, true, nil
		}
		return Toolchain{}, false, nil
	} else if !os.IsNotExist(err) {
		return Toolchain{}, false, fmt.Errorf("reading .python-version: %w", err)
	}

	return Toolchain{}, false, nil
}

// parsePythonVersionConstraint extracts the lower bound from a requires-python
// constraint string such as ">=3.11,<4" or "~=3.11". Returns the raw version
// string for further parsing.
func parsePythonVersionConstraint(constraint string) string {
	// Scan the constraint for the first version number after any operator prefix.
	f := bufio.NewScanner(strings.NewReader(constraint))
	f.Split(bufio.ScanWords)
	for f.Scan() {
		tok := f.Text()
		// Strip comparison operators from the token.
		tok = strings.TrimLeft(tok, "><=~^! ")
		// Strip trailing comma separator.
		tok = strings.TrimRight(tok, ",")
		if rePythonVersion.MatchString(tok) {
			return tok
		}
	}
	return constraint
}
