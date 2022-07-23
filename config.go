package main

import (
	"encoding/json"
	"fmt"
	"os"
)

var config = struct {
	ReposDir       string   `json:"reposDir"`
	OutputDir      string   `json:"outputDir"`
	Pinned         []string `json:"pinned"`
	GitDir         string   `json:"gitDir"`
	IndexFile      string   `json:"indexFile"`
	IndexHead      string   `json:"indexHead"`
	IndexFoot      string   `json:"indexFoot"`
	PinClass       string   `json:"pinClass"`
	RepoTemplate   string   `json:"repoTemplate"`
	RepoDateFormat string   `json:"repoDateFormat"`
	PrettyPrint    []string `json:"prettyPrint"`
}{
	ReposDir:  "./",
	OutputDir: ".",
	GitDir:    ".git",
	IndexFile: "index.html",
	IndexHead: `<!DOCTYPE html>
<html lang="en">
        <head>
                <title>Repositories</title>
                <link type="text/css" rel="stylesheet" href="/style/repos.css">
        </head>
        <body>
                <h1>Repositories</h1>
                <ul>`,
	PinClass: " class=\"pinned\"",
	RepoTemplate: `
                        <li%s>
                                <a href=%q>%s</a>
                                <span>%s</span>
                                <span>Latest Commit:</span><span>%s: %s</span>
                        </li>`,
	RepoDateFormat: "2006/01/02 15:04:05",
	IndexFoot: `
                </ul>
        </body>
</html>`,
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
	return nil
}
