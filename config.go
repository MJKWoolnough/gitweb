package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"strings"

	"vimagination.zapto.org/parser"
)

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
		"repeat": strings.Repeat,
		"split":  strings.Split,
	}
	config = struct {
		ReposDir                                    string   `json:"reposDir"`
		OutputDir                                   string   `json:"outputDir"`
		Pinned                                      []string `json:"pinned"`
		GitDir                                      string   `json:"gitDir"`
		IndexFile                                   string   `json:"indexFile"`
		IndexTemplate                               string   `json:"indexTemplate"`
		IndexTemplateFile                           string   `json:"indexTemplateFile"`
		RepoTemplate                                string   `json:"repoTemplate"`
		RepoTemplateFile                            string   `json:"repoTemplateFile"`
		PrettyPrint                                 []string `json:"prettyPrint"`
		PrettyTemplate                              string   `json:"prettyTemplate"`
		PrettyTemplateFile                          string   `json:"prettyTemplateFile"`
		indexTemplate, repoTemplate, prettyTemplate *template.Template
		prettyMap                                   map[string]parser.TokenFunc
	}{
		ReposDir:  "./",
		OutputDir: ".",
		GitDir:    ".git",
		IndexFile: "index.html",
		prettyMap: make(map[string]parser.TokenFunc),
	}
	prettyPrinters = map[string]parser.TokenFunc{
		".go": commentsPlain,
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

	if config.IndexTemplateFile != "" {
		f, err := os.Open(config.IndexTemplateFile)
		if err != nil {
			return fmt.Errorf("error opening index template file: %w", err)
		}

		b, err := io.ReadAll(f)

		f.Close()

		if err != nil {
			return fmt.Errorf("error reading index template file: %w", err)
		}

		config.IndexTemplate = string(b)
	}

	if config.RepoTemplateFile != "" {
		f, err := os.Open(config.RepoTemplateFile)
		if err != nil {
			return fmt.Errorf("error opening repo template file: %w", err)
		}

		b, err := io.ReadAll(f)

		f.Close()

		if err != nil {
			return fmt.Errorf("error reading repo template file: %w", err)
		}

		config.RepoTemplate = string(b)
	}

	if config.PrettyTemplateFile != "" {
		f, err := os.Open(config.PrettyTemplateFile)
		if err != nil {
			return fmt.Errorf("error opening pretty template file: %w", err)
		}

		b, err := io.ReadAll(f)

		f.Close()

		if err != nil {
			return fmt.Errorf("error reading pretty template file: %w", err)
		}

		config.PrettyTemplate = string(b)
	}

	if config.indexTemplate, err = template.New("index").Funcs(fMap).Parse(config.IndexTemplate); err != nil {
		return fmt.Errorf("error parsing index template: %w", err)
	}

	if config.repoTemplate, err = template.New("repo").Funcs(fMap).Parse(config.RepoTemplate); err != nil {
		return fmt.Errorf("error parsing repo template: %w", err)
	}

	if config.prettyTemplate, err = template.New("pretty").Funcs(fMap).Parse(config.PrettyTemplate); err != nil {
		return fmt.Errorf("error parsing pretty template: %w", err)
	}

	for _, printer := range config.PrettyPrint {
		if p, ok := prettyPrinters[printer]; ok {
			config.prettyMap[printer] = p
		}
	}

	return nil
}
