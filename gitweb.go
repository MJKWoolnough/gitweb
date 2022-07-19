package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"time"
)

var config = struct {
	ReposDir  string   `json:"reposDir"`
	OutputDir string   `json:"outpurDir"`
	Pinned    []string `json:"pinned"`
	GitDir    string   `json:"gitDir"`
}{
	ReposDir:  "./",
	OutputDir: ".",
	GitDir:    ".git",
}

type commit struct {
	tree, parent, msg string
	time              time.Time
}

func readLatestCommit(dir string) (*commit, error) {
	return nil, nil
}

func main() {
	u, err := user.Current()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting current user: %s", err)
		os.Exit(1)
	}
	configFile := flag.String("c", filepath.Join(u.HomeDir, ".gitweb"), "config file location")
	gitDir := flag.String("r", "", "git repo to build")
	flag.Parse()
	if err := readConfig(*configFile); err != nil {
		fmt.Fprintf(os.Stderr, "error reading config: %s", err)
		os.Exit(2)
	}
	if *gitDir != "" {
		if err := buildRepo(*gitDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	// build index
	if err := buildIndex(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
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

func buildRepo(repo string) error {
	return nil
}

func checkSHA(sha []byte) string {
	for _, c := range sha {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return ""
		}
	}
	return string(sha)
}

type repo struct {
	name, desc, lastCommit string
	lastCommitTime         time.Time
	pin                    int
}

func buildIndex() error {
	dir, err := os.ReadDir(config.ReposDir)
	if err != nil {
		return fmt.Errorf("error reading repos dir: %w", err)
	}
	repos := make([]repo, 0, len(dir))
	for _, r := range dir {
		if r.Type()&fs.ModeDir != 0 {
			name := r.Name()
			rGit := filepath.Join(config.ReposDir, name, config.GitDir)
			c, err := readLatestCommit(rGit)
			if err == nil {
				pinPos := -1
				for n, m := range config.Pinned {
					if m == name {
						pinPos = n
						break
					}
				}
				desc := ""
				f, err := os.Open(filepath.Join(rGit, "description"))
				if err == nil {
					d, err := io.ReadAll(f)
					f.Close()
					if err == nil {
						if string(d) != defaultDesc {
							desc = string(d[:len(d)-1])
						}
					}
				}
				repos = append(repos, repo{
					name:           name,
					desc:           desc,
					lastCommit:     c.msg,
					lastCommitTime: c.time,
					pin:            pinPos,
				})
			}
		}
	}
	f, err := os.Create(filepath.Join(config.OutputDir, "index.html"))
	if err != nil {
		return fmt.Errorf("error creating index: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(indexHead); err != nil {
		return fmt.Errorf("error writing index header: %w", err)
	}
	sort.Slice(repos, func(i, j int) bool {
		ir := repos[i]
		jr := repos[j]
		if ir.pin == -1 && jr.pin == -1 {
			return ir.lastCommitTime.After(jr.lastCommitTime)
		} else if ir.pin == -1 {
			return false
		} else if jr.pin == -1 {
			return true
		}
		return ir.pin < jr.pin
	})
	for _, r := range repos {
		pinned := ""
		if r.pin != -1 {
			pinned = pinClass
		}
		if _, err := fmt.Fprintf(f, repoTemplate, pinned, "/"+r.name+"/", html.EscapeString(r.name), html.EscapeString(r.desc), r.lastCommitTime.Format(repoDateFormat), html.EscapeString(r.lastCommit)); err != nil {
			return fmt.Errorf("error while writing index: %w", err)
		}
	}
	if _, err := f.WriteString(indexFoot); err != nil {
		return fmt.Errorf("error writing index footer: %w", err)
	}
	return nil
}

const (
	indexHead = `<!DOCTYPE html>
<html lang="en">
        <head>
                <title>Repositories</title>
                <link type="text/css" rel="stylesheet" href="/style/repos.css">
        </head>
        <body>
                <h1>Repositories</h1>
                <ul>`
	pinClass     = " class=\"pinned\""
	repoTemplate = `
                        <li%s>
                                <a href=%q>%s</a>
                                <span>%s</span>
                                <span>Latest Commit:</span><span>%s: %s</span>
                        </li>`
	repoDateFormat = "2006/01/02 15:04:05"
	indexFoot      = `
                </ul>
        </body>
</html>`
	defaultDesc = "Unnamed repository; edit this file 'description' to name the repository.\n"
)
