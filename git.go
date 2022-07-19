package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Repo struct {
	path, head string
}

func OpenRepo(path string) (*Repo, error) {
	f, err := os.Open(filepath.Join(path, "HEAD"))
	if err != nil {
		return nil, fmt.Errorf("error opening HEAD: %w", err)
	}
	defer f.Close()
	var buf [256]byte
	if _, err := io.ReadFull(f, buf[:5]); err != nil {
		return nil, fmt.Errorf("error while reading HEAD: %w", err)
	}
	if string(buf[:5]) != "ref: " {
		return nil, errors.New("invalid HEAD file")
	}
	n, err := io.ReadFull(f, buf[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("error while reading HEAD: %w", err)
	}
	return &Repo{
		path: path,
		head: string(buf[:n-1]),
	}, nil
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

func (r *Repo) GetLatestCommitID() (string, error) {
	f, err := os.Open(filepath.Join(r.path, r.head))
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

func (r *Repo) getObject(id string) (interface{}, error) {
	return nil, nil
}
