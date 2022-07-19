package main

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Repo struct {
	path string
}

func OpenRepo(path string) *Repo {
	return &Repo{
		path: path,
	}
}

func (r *Repo) GetDescription() string {
	var desc string
	f, err := os.Open(filepath.Join(r.path, "description"))
	if err == nil {
		d, err := io.ReadAll(f)
		f.Close()
		if err == nil {
			if string(d) != defaultDesc {
				desc = string(d[:len(d)-1])
			}
		}
	}
	return desc
}

func (r *Repo) readHeadRef() (string, error) {
	f, err := os.Open(filepath.Join(r.path, "HEAD"))
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

func (r *Repo) GetLatestCommitID() (string, error) {
	head, err := r.readHeadRef()
	if err != nil {
		return "", err
	}
	f, err := os.Open(filepath.Join(r.path, head))
	if err != nil {
		return "", fmt.Errorf("error opening ref: %w", err)
	}
	defer f.Close()
	var buf [256]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("error while reading ref: %w", err)
	}
	return string(buf[:n-1]), nil
}

func (r *Repo) getObject(id string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(r.path, "objects", id[:2], id[2:]))
	if err != nil {
		return nil, fmt.Errorf("error opening object file (%s): %w", id, err)
	}
	z, err := zlib.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("error decompressing object file (%s): %s", id, err)
	}
	return struct {
		io.Reader
		io.Closer
	}{
		Reader: z,
		Closer: f,
	}, nil
}

type Commit struct {
	Tree, Parent, Msg string
	Time              time.Time
}

func (r *Repo) GetCommit(id string) (*Commit, error) {
	o, err := r.getObject(id)
	if err != nil {
		return nil, fmt.Errorf("error while opening lastCommit: %w", err)
	}
	buf, err := io.ReadAll(o)
	o.Close()
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
	c := new(Commit)
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
			if c.Tree == "" {
				if c.Tree = checkSHA(line[5:]); c.Tree == "" {
					return nil, errors.New("invalid tree SHA")
				}
			}
		} else if p > 7 && string(line[:7]) == "parent " {
			if c.Parent == "" {
				if c.Parent = checkSHA(line[7:]); c.Parent == "" {
					return nil, errors.New("invalid parent SHA")
				}
			}
		} else if p > 10 && string(line[:10]) == "committer " {
			if c.Time.IsZero() {
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
				c.Time = time.Unix(unix, 0).In(time.FixedZone("UTC", int(hours*3600+mins*60)))
			}
		}
	}
	c.Msg = string(buf[:len(buf)-1])
	return c, nil
}
