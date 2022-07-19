package main

import (
	"flag"
	"fmt"
	"html"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"time"
)

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

func buildRepo(repo string) error {
	return nil
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
			rp := OpenRepo(filepath.Join(config.ReposDir, name, config.GitDir))
			cid, err := rp.GetLatestCommitID()
			var c *Commit
			if err == nil {
				c, err = rp.GetCommit(cid)
			}
			if err == nil {
				pinPos := -1
				for n, m := range config.Pinned {
					if m == name {
						pinPos = n
						break
					}
				}
				repos = append(repos, repo{
					name:           name,
					desc:           rp.GetDescription(),
					lastCommit:     c.Msg,
					lastCommitTime: c.Time,
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
	if _, err := f.WriteString(config.IndexHead); err != nil {
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
			pinned = config.PinClass
		}
		if _, err := fmt.Fprintf(f, config.RepoTemplate, pinned, "/"+r.name+"/", html.EscapeString(r.name), html.EscapeString(r.desc), r.lastCommitTime.Format(config.RepoDateFormat), html.EscapeString(r.lastCommit)); err != nil {
			return fmt.Errorf("error while writing index: %w", err)
		}
	}
	if _, err := f.WriteString(config.IndexFoot); err != nil {
		return fmt.Errorf("error writing index footer: %w", err)
	}
	return nil
}

const (
	defaultDesc = "Unnamed repository; edit this file 'description' to name the repository.\n"
)
