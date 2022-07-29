package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
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
	Path  []string
	Dirs  map[string]*Dir
	Files map[string]*File
}

type File struct {
	Repo, Name, Path, Link, Ext string
	Commit                      *Commit
	Size                        int64
}

type Discard struct {
	io.Writer
}

func (Discard) Close() error {
	return nil
}

var discard = Discard{Writer: io.Discard}

func parseTree(repo string, r *Repo, tree Tree, p []string) (*Dir, error) {
	basepath := filepath.Join(append(append(make([]string, len(p)+3), config.OutputDir, repo, "files"), p...)...)
	if err := os.MkdirAll(basepath, 0o755); err != nil {
		return nil, fmt.Errorf("error creating directories: %w", err)
	}
	files, err := os.ReadDir(basepath)
	if err != nil {
		return nil, fmt.Errorf("error reading file directory: %w", err)
	}
	fileMap := make(map[string]struct{}, len(files))
	for _, file := range files {
		fileMap[file.Name()] = struct{}{}
	}
	dir := &Dir{
		Dirs:  make(map[string]*Dir),
		Files: make(map[string]*File),
		Path:  append(make([]string, 0, len(p)), p...),
	}
	for _, f := range sortedFiles(tree) {
		if f[len(f)-1] == '/' {
			nt, err := r.GetTree(tree[f])
			if err != nil {
				return nil, fmt.Errorf("error reading tree: %w", err)
			}
			d, err := parseTree(repo, r, nt, append(p, f))
			if err != nil {
				return nil, fmt.Errorf("error parsing dir: %w", err)
			}
			d.ID = tree[f]
			dir.Dirs[f[:len(f)-1]] = d
			delete(fileMap, f[:len(f)-1])
		} else {
			fpath := append(p, f)
			c, err := getFileLastCommit(r, fpath)
			if err != nil {
				return nil, fmt.Errorf("error reading files last commit: %w", err)
			}
			name := f
			file := &File{
				Repo:   repo,
				Name:   name,
				Path:   path.Join(fpath...),
				Ext:    filepath.Ext(name),
				Commit: c,
			}
			if f[0] == '/' {
				name = f[1:]
				b, err := r.GetBlob(tree[f])
				if err != nil {
					return nil, fmt.Errorf("error getting symlink data: %w", err)
				}
				d, err := io.ReadAll(b)
				if err != nil {
					b.Close()
					return nil, fmt.Errorf("error reading symlink data: %w", err)
				}
				b.Close()
				file.Link = string(d)
			} else {
				output := true
				outpath := filepath.Join(basepath, name)
				b, err := r.GetBlob(tree[f])
				var o io.WriteCloser
				if err != nil {
					return nil, fmt.Errorf("error getting file data: %w", err)
				}
				if _, ok := fileMap[name]; ok {
					fi, err := os.Stat(outpath)
					if err != nil {
						return nil, fmt.Errorf("error while stat'ing file: %w", err)
					}
					if fi.ModTime().Equal(c.Time) {
						output = false
						o = discard
					}
				}
				printer := passThru
				if output {
					o, err = os.Create(outpath)
					if err != nil {
						return nil, fmt.Errorf("error creating data file: %w", err)
					}
					if p, ok := config.prettyMap[file.Ext]; ok {
						printer = p
					}
				}
				if file.Size, err = printer(file, o, b); err != nil {
					o.Close()
					return nil, fmt.Errorf("error writing file data: %w", err)
				}
				if err := o.Close(); err != nil {
					return nil, fmt.Errorf("error closing file: %w", err)
				}
				if output {
					if err := os.Chtimes(outpath, c.Time, c.Time); err != nil {
						return nil, fmt.Errorf("error setting file time: %w", err)
					}
				}
			}
			dir.Files[name] = file
			delete(fileMap, name)
		}
	}
	for f := range fileMap {
		if err := os.Remove(filepath.Join(basepath, f)); err != nil {
			return nil, fmt.Errorf("error removing file: %w", err)
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
	d, err := parseTree(repo, r, tree, []string{})
	if err != nil {
		return err
	}
	index, err := os.Create(filepath.Join(config.OutputDir, repo, "index.html"))
	if err != nil {
		return fmt.Errorf("error creating repo index: %w", err)
	}
	if err := config.repoTemplate.Execute(index, RepoInfo{
		Name: repo,
		Desc: r.GetDescription(),
		Root: d,
	}); err != nil {
		index.Close()
		return fmt.Errorf("error processing repo template: %w", err)
	}
	if err = index.Close(); err != nil {
		return fmt.Errorf("error closing index: %w", err)
	}
	return nil
}

type RepoData struct {
	Name, Desc, LastCommit string
	LastCommitTime         time.Time
	Pin                    int
}

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
	defer f.Close()
	if err := config.indexTemplate.Execute(f, repos); err != nil {
		return fmt.Errorf("error processing template: %w", err)
	}
	return nil
}
