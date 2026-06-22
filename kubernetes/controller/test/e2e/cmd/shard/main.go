// Command shard enumerates e2e scenarios under examples/ and
// test/e2e/scenarios/, splits them into shards, and emits a
// matrix=<json> line suitable for $GITHUB_OUTPUT.
//
// By default (--shards=0), each scenario gets its own shard. Pass
// --shards=N to group scenarios into N round-robin buckets instead.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const e2eYamlFile = "e2e.yaml"

func main() {
	shards := flag.Int("shards", 0, "number of shards to split scenarios into (0 = one shard per scenario)")
	repoRoot := flag.String("repo-root", "", "path to the controller module root (defaults to cwd)")
	flag.Parse()

	root := *repoRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "getwd:", err)
			os.Exit(1)
		}
	}

	roots := []string{
		filepath.Join(root, "examples"),
		filepath.Join(root, "test", "e2e", "scenarios"),
	}

	var scenarios []string
	for _, r := range roots {
		found, err := walk(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "walk %s: %v\n", r, err)
			os.Exit(1)
		}
		scenarios = append(scenarios, found...)
	}
	sort.Strings(scenarios)

	numShards := *shards
	if numShards <= 0 {
		numShards = len(scenarios)
	}
	if numShards < 1 {
		numShards = 1
	}

	type shard struct {
		Shard int    `json:"shard"`
		Focus string `json:"focus"`
	}
	matrix := make([]shard, numShards)
	for i := range matrix {
		matrix[i] = shard{Shard: i, Focus: ""}
	}

	// Round-robin scenarios into shards. Empty shards get a sentinel focus
	// that matches nothing so the runner exits quickly.
	buckets := make([][]string, numShards)
	for i, s := range scenarios {
		buckets[i%numShards] = append(buckets[i%numShards], s)
	}
	for i, names := range buckets {
		if len(names) == 0 {
			matrix[i].Focus = "$.^" // matches nothing
			continue
		}
		// Output plain scenario name when a single scenario per shard;
		// the Taskfile anchors it automatically. For multi-scenario shards,
		// join with | so the Taskfile passes the pre-built regex through.
		if len(names) == 1 {
			matrix[i].Focus = names[0]
		} else {
			matrix[i].Focus = "^.*(" + joinAlternation(names) + ")$"
		}
	}

	out, err := json.Marshal(matrix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "matrix=%s\n", out) //nolint:errcheck // stdout write in CLI main, no actionable recovery
}

func walk(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, e2eYamlFile)); statErr == nil {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found = append(found, filepath.ToSlash(rel))
			return fs.SkipDir
		}
		return nil
	})
	return found, err
}

func joinAlternation(names []string) string {
	// Names contain only path characters and slashes; no regex
	// metacharacters need escaping. If a future folder uses dots or
	// other meta, harden this with regexp.QuoteMeta.
	return strings.Join(names, "|")
}
