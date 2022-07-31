package main

import (
	"encoding/json"
	"fmt"
	"html/template"
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
	config.indexTemplate = template.New("index").Funcs(fMap)
	if config.IndexTemplateFile != "" {
		if _, err = config.indexTemplate.ParseFiles(config.IndexTemplateFile); err != nil {
			return fmt.Errorf("error parsing index template file: %w", err)
		}
	} else if _, err = config.indexTemplate.Parse(config.IndexTemplate); err != nil {
		return fmt.Errorf("error parsing index template: %w", err)
	}
	config.repoTemplate = template.New("repo").Funcs(fMap)
	if config.RepoTemplateFile != "" {
		if _, err = config.repoTemplate.ParseFiles(config.RepoTemplateFile); err != nil {
			return fmt.Errorf("error parsing repo template file: %w", err)
		}
	} else if _, err = config.repoTemplate.Parse(config.RepoTemplate); err != nil {
		return fmt.Errorf("error parsing repo template: %w", err)
	}
	config.prettyTemplate = template.New("pretty").Funcs(fMap)
	if config.PrettyTemplateFile != "" {
		if _, err = config.prettyTemplate.ParseFiles(config.PrettyTemplateFile); err != nil {
			return fmt.Errorf("error parsing pretty template file: %w", err)
		}
	} else if _, err = config.prettyTemplate.Parse(config.PrettyTemplate); err != nil {
		return fmt.Errorf("error parsing repo template: %w", err)
	}
	for _, printer := range config.PrettyPrint {
		if p, ok := prettyPrinters[printer]; ok {
			config.prettyMap[printer] = p
		}
	}
	return nil
}
