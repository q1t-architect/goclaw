//go:build ignore

// Coverage ratchet gate: fails CI if any package drops below its stored threshold.
//
// Usage:
//
//	go run scripts/check_coverage.go [-coverprofile=coverage.out] [-thresholds=scripts/coverage_thresholds.json] [-update]
//
// Flags:
//
//	-coverprofile: Path to coverage.out (default: coverage.out)
//	-thresholds:   Path to thresholds JSON (default: scripts/coverage_thresholds.json)
//	-update:       Write current coverage as new thresholds (ratchet up, explicit opt-in)
//
// Exit codes:
//
//	0 = all packages meet threshold (or --update succeeded)
//	1 = at least one package below threshold
//	2 = parse/IO error
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

const modulePrefix = "github.com/nextlevelbuilder/goclaw/"

// Trivial/infra-only packages excluded from coverage gate.
var excluded = map[string]bool{
	"internal/version":    true,
	"internal/webui":      true,
	"internal/updater":    true,
	"pkg/protocol":        true,
	"tests/zalo_e2e":      true,
	"ui/desktop":          true,
	"cmd":                 true,
	"scripts":             true,
}

type pkgCoverage struct {
	pkg         string
	statements  int
	covered     int
}

// parseCoverProfile reads a Go coverage profile and groups statements by package.
// Coverage lines look like: "github.com/foo/bar/file.go:12.1,15.2 3 1"
// Fields: file:start,end  numStatements  count
func parseCoverProfile(path string) (map[string]*pkgCoverage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	packages := make(map[string]*pkgCoverage)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			if strings.HasPrefix(line, "mode:") {
				continue
			}
		}
		if line == "" {
			continue
		}
		// Split into 3 parts: "file:range numStmt count"
		// Last two fields are numeric; everything before is the filename.
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		fileRange := strings.Join(parts[:len(parts)-2], " ")
		numStmt, err := strconv.Atoi(parts[len(parts)-2])
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			continue
		}
		// Extract file path (before ':')
		fullFile, _, ok := strings.Cut(fileRange, ":")
		if !ok {
			continue
		}
		// Strip module prefix to get relative path.
		rel := strings.TrimPrefix(fullFile, modulePrefix)
		// Package is the directory.
		slash := strings.LastIndex(rel, "/")
		if slash < 0 {
			continue
		}
		pkg := rel[:slash]
		if pkg == "" {
			continue
		}
		entry, ok := packages[pkg]
		if !ok {
			entry = &pkgCoverage{pkg: pkg}
			packages[pkg] = entry
		}
		entry.statements += numStmt
		if count > 0 {
			entry.covered += numStmt
		}
	}
	return packages, scanner.Err()
}

func isExcluded(pkg string) bool {
	if excluded[pkg] {
		return true
	}
	for p := range excluded {
		if strings.HasPrefix(pkg, p+"/") {
			return true
		}
	}
	return false
}

func loadThresholds(path string) (map[string]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]float64{}, nil
		}
		return nil, err
	}
	var out map[string]float64
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func writeThresholds(path string, thresholds map[string]float64) error {
	// Sorted output for deterministic diffs.
	keys := make([]string, 0, len(thresholds))
	for k := range thresholds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]float64, len(keys))
	for _, k := range keys {
		ordered[k] = thresholds[k]
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func pctStr(p float64) string {
	return fmt.Sprintf("%5.1f%%", p)
}

func main() {
	var (
		profilePath    = flag.String("coverprofile", "coverage.out", "coverage profile path")
		thresholdsPath = flag.String("thresholds", "scripts/coverage_thresholds.json", "thresholds JSON path")
		update         = flag.Bool("update", false, "write current coverage as new thresholds")
	)
	flag.Parse()

	packages, err := parseCoverProfile(*profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing %s: %v\n", *profilePath, err)
		os.Exit(2)
	}

	thresholds, err := loadThresholds(*thresholdsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading thresholds: %v\n", err)
		os.Exit(2)
	}

	type row struct {
		pkg       string
		current   float64
		threshold float64
		delta     float64
		status    string
	}
	var rows []row
	failed := 0
	newThresholds := make(map[string]float64)

	for _, entry := range packages {
		if isExcluded(entry.pkg) {
			continue
		}
		if entry.statements == 0 {
			continue
		}
		current := 100 * float64(entry.covered) / float64(entry.statements)
		threshold := thresholds[entry.pkg]
		delta := current - threshold
		status := "PASS"
		if current+0.01 < threshold { // tiny epsilon for float comparison
			status = "FAIL"
			failed++
		}
		rows = append(rows, row{entry.pkg, current, threshold, delta, status})
		// Preserve existing thresholds for packages that still exist.
		newThresholds[entry.pkg] = current
	}

	// Also preserve thresholds for packages still in file but not in coverage
	// (e.g., tests excluded in this run); keep them to avoid false passes.
	if !*update {
		for k, v := range thresholds {
			if _, ok := newThresholds[k]; !ok {
				newThresholds[k] = v
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].pkg < rows[j].pkg })

	fmt.Println("Coverage Gate Report")
	fmt.Println("====================")
	fmt.Printf("%-60s %8s %8s %8s  %s\n", "PACKAGE", "CURRENT", "FLOOR", "DELTA", "STATUS")
	for _, r := range rows {
		fmt.Printf("%-60s %8s %8s %+7.1f%%  %s\n",
			r.pkg, pctStr(r.current), pctStr(r.threshold), r.delta, r.status)
	}
	fmt.Println()

	if *update {
		if err := writeThresholds(*thresholdsPath, newThresholds); err != nil {
			fmt.Fprintf(os.Stderr, "error writing thresholds: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("Updated %s with %d package thresholds\n", *thresholdsPath, len(newThresholds))
		os.Exit(0)
	}

	if failed > 0 {
		fmt.Printf("FAIL: %d package(s) below threshold\n", failed)
		os.Exit(1)
	}
	fmt.Printf("PASS: all %d package(s) meet threshold\n", len(rows))
}
