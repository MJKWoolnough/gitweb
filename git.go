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
	"sync"
	"time"

	"vimagination.zapto.org/byteio"
)

type Repo struct {
	path        string
	loadPacks   sync.Once
	packsErr    error
	packObjects map[string]packObject
}

type packObject struct {
	pack   string
	offset uint64
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
	id := checkSHA(buf[:n-1])
	if id == "" {
		return "", errors.New("invalid id")
	}
	return id, nil
}

var newLine = []byte{'\n'}

func (r *Repo) loadPacksData() {
	f, err := os.Open(filepath.Join(r.path, "objects", "info", "packs"))
	if err != nil {
		if !os.IsNotExist(err) {
			r.packsErr = fmt.Errorf("error opening packs file: %w", err)
		}
		return
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		r.packsErr = fmt.Errorf("error reading packs file: %w", err)
		return
	}
	packs := bytes.Split(data, newLine)
	r.packObjects = make(map[string]packObject)
	for _, p := range packs {
		if len(p) > 5 && p[0] == 'P' && p[1] == ' ' && string(p[len(p)-5:]) == ".pack" {
			pack := string(p[2:])
			idx, err := os.Open(filepath.Join(r.path, "objects", "pack", pack[:len(pack)-4]+"idx"))
			if err != nil {
				r.packsErr = fmt.Errorf("error opening pack index for %s: %w", pack, err)
				return
			}
			sidx := byteio.StickyBigEndianReader{Reader: idx}
			a := sidx.ReadUint32()
			if a == 4285812579 { // 0xff + 't0c'
				if version := sidx.ReadUint32(); version != 2 {
					idx.Close()
					r.packsErr = fmt.Errorf("unsupported version number (%d) in pack index for %s", version, pack)
					return
				}
				io.CopyN(io.Discard, &sidx, 4*255) // ignore fan
				a = sidx.ReadUint32()
				names := make([]string, a)
				var name [20]byte
				for n := range names {
					sidx.Read(name[:])
					names[n] = fmt.Sprintf("%x", name)
				}
				io.CopyN(io.Discard, &sidx, 4*int64(a)) // ignore CRC32's
				larger := make(map[uint32]string)
				var largest uint32
				for _, name := range names {
					offset := sidx.ReadUint32()
					if offset&0x80000000 != 0 {
						index := offset & 0x7fffffff
						if largest <= index {
							largest = index + 1
						}
						larger[index] = name
					} else {
						r.packObjects[name] = packObject{
							pack:   pack,
							offset: uint64(offset),
						}
					}
				}
				for i := uint32(0); i < largest && sidx.Err == nil; i++ {
					offset := sidx.ReadUint64()
					if name, ok := larger[i]; ok {
						r.packObjects[name] = packObject{
							pack:   pack,
							offset: offset,
						}
					}
				}
			} else {
				r.packsErr = fmt.Errorf("version 1 unsupported in pack index for %s", pack)
			}
			idx.Close()
			if sidx.Err != nil {
				r.packsErr = fmt.Errorf("error reading pack index for %s: %w", pack, sidx.Err)
				return
			}
		}
	}
}

func (r *Repo) getObject(id string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(r.path, "objects", id[:2], id[2:]))
	if os.IsNotExist(err) {
		r.loadPacks.Do(r.loadPacksData)
		if r.packsErr != nil {
			err = r.packsErr
		} else {
			// open from pack
		}
	}
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
		return nil, fmt.Errorf("error while opening commit object: %w", err)
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

type Tree []TreeObject

type TreeObject struct {
	Name, Object string
}

func (r *Repo) GetTree(id string) (Tree, error) {
	o, err := r.getObject(id)
	if err != nil {
		return nil, fmt.Errorf("error while opening tree object: %w", err)
	}
	buf, err := io.ReadAll(o)
	o.Close()
	if err != nil {
		return nil, err
	}
	if string(buf[:5]) != "tree " {
		return nil, errors.New("not a tree")
	}
	buf = buf[5:]
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
		return nil, errors.New("zero tree size")
	} else if l != uint64(len(buf)) {
		return nil, errors.New("invalid tree size")
	}
	var files Tree
	for len(buf) > 0 {
		p := bytes.IndexByte(buf, ' ')
		if p == -1 {
			return nil, errors.New("unable to read file mode")
		}
		mode := buf[:p]
		buf = buf[p+1:]
		p = bytes.IndexByte(buf, 0)
		if p == -1 {
			return nil, errors.New("unable to read file mode")
		}
		name := string(buf[:p])
		if string(mode) == "40000" {
			name += "/"
		}
		buf = buf[p+1:]
		files = append(files, TreeObject{
			Name:   name,
			Object: fmt.Sprintf("%x", buf[:20]),
		})
		buf = buf[20:]
	}
	return files, nil
}

func checkSHA(sha []byte) string {
	for _, c := range sha {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return ""
		}
	}
	return string(sha)
}
