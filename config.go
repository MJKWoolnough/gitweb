package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"
)

type printer func(w io.Writer, r io.Reader) (int64, error)

var (
	fMap = template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"mul": func(a, b int) int {
			return a * b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"indent": func(n int) string {
			return strings.Repeat("	", n)
		},
	}
	config = struct {
		ReposDir                                    string   `json:"reposDir"`
		OutputDir                                   string   `json:"outputDir"`
		Pinned                                      []string `json:"pinned"`
		GitDir                                      string   `json:"gitDir"`
		IndexFile                                   string   `json:"indexFile"`
		IndexTemplate                               string   `json:"indexTemplate"`
		RepoTemplate                                string   `json:"repoTemplate"`
		PrettyPrint                                 []string `json:"prettyPrint"`
		PrettyTemplate                              string   `json:"prettyTemplate"`
		indexTemplate, repoTemplate, prettyTemplate *template.Template
		prettyMap                                   map[string]printer
	}{
		ReposDir:  "./",
		OutputDir: ".",
		GitDir:    ".git",
		IndexFile: "index.html",
		prettyMap: make(map[string]printer),
	}
	prettyPrinters = map[string]printer{
		".go": io.Copy,
		".ts": io.Copy,
	}
)

func readConfig(configFile string) error {
	f, err := os.Open(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("error while opening config file: %w", err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		return fmt.Errorf("error parsing config file: %w", err)
	}
	if config.indexTemplate, err = template.New("index").Funcs(fMap).Parse(config.IndexTemplate); err != nil {
		return fmt.Errorf("error parsing index template: %w", err)
	}
	if config.repoTemplate, err = template.New("repo").Funcs(fMap).Parse(config.RepoTemplate); err != nil {
		return fmt.Errorf("error parsing repo template: %w", err)
	}
	if config.prettyTemplate, err = template.New("pretty").Funcs(fMap).Parse(config.PrettyTemplate); err != nil {
		return fmt.Errorf("error parsing repo template: %w", err)
	}
	for _, printer := range config.PrettyPrint {
		if p, ok := prettyPrinters[printer]; ok {
			config.prettyMap[printer] = p
		}
	}
	return nil
}
