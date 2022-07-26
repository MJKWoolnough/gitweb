package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path"
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

func getFileLastCommit(r *Repo, path []string) (*Commit, error) {
	cid, err := r.GetLatestCommitID()
	if err != nil {
		return nil, fmt.Errorf("error reading last commit id: %w", err)
	}
	last, err := r.GetCommit(cid)
	if err != nil {
		return nil, fmt.Errorf("error reading commit: %w", err)
	}
	objID := last.Tree
	for _, p := range path {
		t, err := r.GetTree(objID)
		if err != nil {
			return nil, fmt.Errorf("error reading tree: %w", err)
		}
		nID, ok := t[p]
		if !ok {
			return nil, errors.New("invalid file")
		}
		objID = nID
	}
	for {
		c, err := r.GetCommit(cid)
		if err != nil {
			return nil, fmt.Errorf("error reading commit: %w", err)
		}
		tID := c.Tree
		for _, p := range path {
			t, err := r.GetTree(tID)
			if err != nil {
				return nil, fmt.Errorf("error reading tree: %w", err)
			}
			nID, ok := t[p]
			if !ok {
				return last, nil
			}
			tID = nID
		}
		if tID != objID {
			return last, nil
		}
		cid = c.Parent
		last = c
	}
}

type files []string

func (f files) Len() int {
	return len(f)
}

func (f files) Less(i, j int) bool {
	a := f[i]
	b := f[j]
	if a[len(a)-1] == '/' {
		if b[len(b)-1] == '/' {
			return a < b
		}
		return true
	} else if b[len(b)-1] == '/' {
		return false
	}
	return a < b
}

func (f files) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}

func sortedFiles(t Tree) files {
	files := make(files, 0, len(t))
	for f := range t {
		files = append(files, f)
	}
	sort.Sort(files)
	return files
}

type Dir struct {
	ID    string
	Dirs  map[string]*Dir
	Files map[string]*File
}

type File struct {
	Path   string
	Commit *Commit
	Size   uint64
}

func parseTree(r *Repo, tree Tree, p []string) (*Dir, error) {
	dir := &Dir{
		Dirs:  make(map[string]*Dir),
		Files: make(map[string]*File),
	}
	for _, f := range sortedFiles(tree) {
		if f[len(f)-1] == '/' {
			nt, err := r.GetTree(tree[f])
			if err != nil {
				return nil, fmt.Errorf("error reading tree: %w", err)
			}
			d, err := parseTree(r, nt, append(p, f))
			if err != nil {
				return nil, fmt.Errorf("error parsing dir: %w", err)
			}
			d.ID = tree[f]
			dir.Dirs[f[:len(f)-1]] = d
		} else {
			name := f
			if f[0] == '/' {
				// Symlink
				name = f[1:]
			}
			fpath := append(p, f)
			c, err := getFileLastCommit(r, fpath)
			if err != nil {
				return nil, fmt.Errorf("error reading files last commit: %w", err)
			}
			dir.Files[name] = &File{
				Path:   path.Join(fpath...),
				Commit: c,
			}
		}
	}
	return dir, nil
}

type RepoInfo struct {
	Name, Desc string
	Root       *Dir
}

func buildRepo(repo string) error {
	r := OpenRepo(filepath.Join(config.ReposDir, repo, config.GitDir))
	cid, err := r.GetLatestCommitID()
	if err != nil {
		return fmt.Errorf("error reading last commit id: %w", err)
	}
	latest, err := r.GetCommit(cid)
	if err != nil {
		return fmt.Errorf("error reading commit: %w", err)
	}
	tree, err := r.GetTree(latest.Tree)
	if err != nil {
		return fmt.Errorf("error reading tree: %w", err)
	}
	d, err := parseTree(r, tree, []string{})
	if err != nil {
		return err
	}
	if err := config.repoTemplate.Execute(os.Stdout, RepoInfo{
		Name: repo,
		Desc: r.GetDescription(),
		Root: d,
	}); err != nil {
		return fmt.Errorf("error processing repo template: %w", err)
	}
	return nil
}

type RepoData struct {
	Name, Desc, LastCommit string
	LastCommitTime         time.Time
	Pin                    int
}

type indexData struct{}

func buildIndex() error {
	dir, err := os.ReadDir(config.ReposDir)
	if err != nil {
		return fmt.Errorf("error reading repos dir: %w", err)
	}
	repos := make([]RepoData, 0, len(dir))
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
				repos = append(repos, RepoData{
					Name:           name,
					Desc:           rp.GetDescription(),
					LastCommit:     c.Msg,
					LastCommitTime: c.Time,
					Pin:            pinPos,
				})
			}
		}
	}
	sort.Slice(repos, func(i, j int) bool {
		ir := repos[i]
		jr := repos[j]
		if ir.Pin == -1 && jr.Pin == -1 {
			return ir.LastCommitTime.After(jr.LastCommitTime)
		} else if ir.Pin == -1 {
			return false
		} else if jr.Pin == -1 {
			return true
		}
		return ir.Pin < jr.Pin
	})
	f, err := os.Create(filepath.Join(config.OutputDir, config.IndexFile))
	if err != nil {
		return fmt.Errorf("error creating index: %w", err)
	}
	if err := config.indexTemplate.Execute(f, repos); err != nil {
		fmt.Errorf("error processing template: %w", err)
	}
	return nil
}

const (
	defaultDesc = "Unnamed repository; edit this file 'description' to name the repository.\n"
)
