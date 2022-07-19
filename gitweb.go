package main

import (
	"bytes"
	"compress/zlib"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

type commit struct {
	tree, parent, msg string
	time              time.Time
}

func readHeadRef(dir string) (string, error) {
	f, err := os.Open(filepath.Join(dir, "HEAD"))
	if err != nil {
		return "", fmt.Errorf("error opening HEAD: %w", err)
	}
	defer f.Close()
	var buf [256]byte
	if _, err := io.ReadFull(f, buf[:5]); err != nil {
		return "", fmt.Errorf("error while reading HEAD: %w", err)
	}
	if string(buf[:5]) != "ref: " {
		return "", errors.New("invalid HEAD file")
	}
	n, err := io.ReadFull(f, buf[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("error while reading HEAD: %w", err)
	}
	return string(buf[:n-1]), nil
}

func parseCommit(r io.Reader) (*commit, error) {
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if string(buf[:7]) != "commit " {
		return nil, errors.New("not a commit")
	}
	buf = buf[7:]
	var l uint64
	for n, c := range buf {
		if c == 0 {
			if l, err = strconv.ParseUint(string(buf[:n]), 10, 64); err != nil {
				return nil, err
			}
			buf = buf[n+1:]
			break
		} else if c < '0' || c > '9' {
			return nil, errors.New("invalid length")
		}
	}
	if l == 0 {
		return nil, errors.New("zero commit size")
	} else if l != uint64(len(buf)) {
		return nil, errors.New("invalid commit size")
	}
	c := new(commit)
	for {
		p := bytes.IndexByte(buf, '\n')
		line := buf[:p]
		buf = buf[p+1:]
		if p == 0 {
			break
		} else if p < 0 {
			return nil, errors.New("invalid commit")
		}
		if p > 5 && string(line[:5]) == "tree " {
			if c.tree == "" {
				if c.tree = checkSHA(line[5:]); c.tree == "" {
					return nil, errors.New("invalid tree SHA")
				}
			}
		} else if p > 7 && string(line[:7]) == "parent " {
			if c.parent == "" {
				if c.parent = checkSHA(line[7:]); c.parent == "" {
					return nil, errors.New("invalid parent SHA")
				}
			}
		} else if p > 10 && string(line[:10]) == "committer " {
			if c.time.IsZero() {
				line = line[10:]
				z := bytes.LastIndexByte(line, ' ')
				if z < 0 {
					return nil, errors.New("invalid timezone")
				}
				zoneOffset, err := strconv.ParseInt(string(line[z+1:]), 10, 16)
				if err != nil {
					return nil, errors.New("invalid timezone string")
				}
				hours := zoneOffset / 100
				mins := zoneOffset % 100
				s := bytes.LastIndexByte(line[:z], ' ')
				if s < 0 {
					return nil, errors.New("invalid timestamp")
				}
				unix, err := strconv.ParseInt(string(line[s+1:z]), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid timestamp string: %w", err)
				}
				c.time = time.Unix(unix, 0).In(time.FixedZone("UTC", int(hours*3600+mins*60)))
			}
		}
	}
	c.msg = string(buf[:len(buf)-1])
	return c, nil
}

func getLatestCommit(dir string) (string, error) {
	head, err := readHeadRef(dir)
	f, err := os.Open(filepath.Join(dir, head))
	if err != nil {
		return "", fmt.Errorf("error opening HEAD ref: %w", err)
	}
	defer f.Close()
	var buf [256]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("error while reading HEAD ref: %w", err)
	}
	return string(buf[:n-1]), nil
}

func readLatestCommit(dir string) (*commit, error) {
	lastCommit, err := getLatestCommit(dir)
	if err != nil {
		return nil, fmt.Errorf("error getting latest commit: %w", err)
	}
	f, err := os.Open(getObjectPath(dir, lastCommit))
	if err != nil {
		return nil, fmt.Errorf("error while opening lastCommit: %w", err)
	}
	z, err := zlib.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("error beginning to decompress lastCommit: %w", err)
	}
	c, err := parseCommit(z)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("error parsing commit: %w", err)
	}
	return c, nil
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

func getObjectPath(gitDir, object string) string {
	return filepath.Join(gitDir, "objects", object[:2], object[2:])
}

const (
	defaultDesc = "Unnamed repository; edit this file 'description' to name the repository.\n"
)
