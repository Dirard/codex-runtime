package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type scaffoldConfig struct {
	source    string
	target    string
	overwrite bool
}

func main() {
	cfg := scaffoldConfig{
		source: filepath.FromSlash("examples/workflows/plain-chat"),
	}
	flag.StringVar(&cfg.source, "source", cfg.source, "starter workflow directory to copy")
	flag.StringVar(&cfg.target, "target", cfg.target, "app-owned target directory to create")
	flag.BoolVar(&cfg.overwrite, "overwrite", false, "allow replacing existing files in the target directory")
	flag.Parse()

	if err := copyWorkflow(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "workflow-scaffold: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("copied workflow scaffold to %s\n", cfg.target)
}

func copyWorkflow(cfg scaffoldConfig) error {
	if strings.TrimSpace(cfg.source) == "" {
		return errors.New("source is required")
	}
	if strings.TrimSpace(cfg.target) == "" {
		return errors.New("target is required")
	}
	sourceInfo, err := os.Stat(cfg.source)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if !sourceInfo.IsDir() {
		return errors.New("source must be a directory")
	}
	if err := os.MkdirAll(cfg.target, 0o755); err != nil {
		return fmt.Errorf("create target: %w", err)
	}
	return filepath.WalkDir(cfg.source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(cfg.source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		targetPath := filepath.Join(cfg.target, rel)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		if !cfg.overwrite {
			if _, err := os.Stat(targetPath); err == nil {
				return fmt.Errorf("target file exists: %s; pass -overwrite to replace it", targetPath)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, contents, 0o644)
	})
}
