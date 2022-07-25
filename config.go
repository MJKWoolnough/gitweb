package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
)

var config = struct {
	ReposDir                    string   `json:"reposDir"`
	OutputDir                   string   `json:"outputDir"`
	Pinned                      []string `json:"pinned"`
	GitDir                      string   `json:"gitDir"`
	IndexFile                   string   `json:"indexFile"`
	IndexTemplate               string   `json:"indexTemplate"`
	RepoTemplate                string   `json:"repoTemplate"`
	PrettyPrint                 []string `json:"prettyPrint"`
	indexTemplate, repoTemplate *template.Template
}{
	ReposDir:  "./",
	OutputDir: ".",
	GitDir:    ".git",
	IndexFile: "index.html",
}

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
	if config.indexTemplate, err = template.New("index").Parse(config.IndexTemplate); err != nil {
		return fmt.Errorf("error parsing index template: %w", err)
	}
	if config.repoTemplate, err = template.New("repo").Parse(config.RepoTemplate); err != nil {
		return fmt.Errorf("error parsing repo template: %w", err)
	}
	return nil
}
