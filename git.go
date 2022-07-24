package main

import (
	"bufio"
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
	"vimagination.zapto.org/memio"
)

const (
	ObjectCommit      = 1
	ObjectTree        = 2
	ObjectBlob        = 3
	ObjectTag         = 4
	ObjectOffsetDelta = 6
	ObjectRefDelta    = 7
)

var (
	objectHeaders = [...]string{
		"",
		"commit ",
		"tree ",
		"blob ",
		"tag ",
	}
	bufPool = sync.Pool{
		New: func() interface{} {
			return new([21]byte)
		},
	}
)

type packObject struct {
	pack   string
	offset uint64
}

type Repo struct {
	path        string
	loadPacks   sync.Once
	packsErr    error
	packObjects map[string]packObject

	cacheMu    sync.RWMutex
	cache      map[string]interface{}
	lastCommit string
}

func OpenRepo(path string) *Repo {
	return &Repo{
		path:  path,
		cache: make(map[string]interface{}),
	}
}

type readCloser struct {
	io.Reader
	io.Closer
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
	r.cacheMu.RLock()
	id := r.lastCommit
	r.cacheMu.RUnlock()
	if id != "" {
		return id, nil
	}
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
	id = checkSHA(buf[:n-1])
	if id == "" {
		return "", errors.New("invalid id")
	}
	r.cacheMu.Lock()
	r.lastCommit = id
	r.cacheMu.Unlock()
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
			sidx := byteio.StickyBigEndianReader{Reader: bufio.NewReader(idx)}
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

func (r *Repo) readPackOffset(p string, o uint64, want int) (io.ReadCloser, error) {
	pack, err := os.Open(filepath.Join(r.path, "objects", "pack", p))
	if err != nil {
		return nil, fmt.Errorf("error opening pack file: %w", err)
	}
	close := true
	defer func() {
		if close {
			pack.Close()
		}
	}()
	var buf [4]byte
	if _, err := pack.Read(buf[:]); err != nil {
		return nil, fmt.Errorf("error reading pack header: %w", err)
	} else if string(buf[:]) != "PACK" {
		return nil, errors.New("invalid pack header")
	}
	if _, err := pack.Read(buf[:]); err != nil {
		return nil, fmt.Errorf("error reading pack version: %w", err)
	} else if buf[0] != 0 || buf[1] != 0 || buf[2] != 0 || buf[3] != 2 {
		return nil, fmt.Errorf("read unsupported pack version: %x", buf)
	}
	if _, err := pack.Seek(int64(o), os.SEEK_SET); err != nil {
		return nil, fmt.Errorf("error seeking to object offset: %w", err)
	}
	if _, err := pack.Read(buf[:1]); err != nil {
		return nil, fmt.Errorf("error reading pack object type: %w", err)
	}
	typ := (buf[0] >> 4) & 7
	if int(typ) != want && typ != ObjectRefDelta && typ != ObjectOffsetDelta {
		return nil, errors.New("wrong packed type")
	}
	size := int64(buf[0] & 15)
	shift := 4
	for buf[0]&0x80 != 0 {
		if _, err := pack.Read(buf[:1]); err != nil {
			return nil, fmt.Errorf("error reading pack object size: %w", err)
		}
		size |= int64(buf[0]&0x7f) << shift
		shift += 7
	}
	var base io.ReadCloser
	switch typ {
	case ObjectCommit, ObjectTree, ObjectBlob:
		z, err := zlib.NewReader(io.LimitReader(pack, size))
		if err != nil {
			return nil, fmt.Errorf("error starting to decompress object: %w", err)
		}
		close = false
		return &readCloser{
			Reader: z,
			Closer: pack,
		}, nil
	case ObjectOffsetDelta:
		ber := byteio.BigEndianReader{Reader: pack}
		baseOffset, _, err := ber.ReadUintX()
		if err != nil {
			return nil, fmt.Errorf("error reading offset: %w", err)
		}
		if baseOffset >= o {
			return nil, errors.New("invalid offset for OffsetDelta")
		}
		if base, err = r.readPackOffset(p, o-baseOffset, want); err != nil {
			return nil, fmt.Errorf("error reading base object: %w", err)
		}
	case ObjectRefDelta:
		var ref [20]byte
		if _, err := pack.Read(ref[:]); err != nil {
			return nil, fmt.Errorf("error reading delta ref: %w", err)
		}
		base, err = r.getObject(string(ref[:]), want)
		if err != nil {
			return nil, fmt.Errorf("error reading base object: %w", err)
		}
	default:
		return nil, errors.New("invalid pack type")
	}
	z, err := zlib.NewReader(io.LimitReader(pack, size))
	if err != nil {
		return nil, fmt.Errorf("error starting to decompress object: %w", err)
	}
	b := byteio.StickyLittleEndianReader{Reader: bufio.NewReader(z)}
	var bSize uint64
	bs := byte(0x80)
	shift = 0
	for bs&0x80 != 0 {
		bs = b.ReadUint8()
		bSize |= uint64(bs&0x7f) << shift
		shift += 7
	}
	var baseBuf memio.LimitedBuffer
	switch base := base.(type) {
	case *memio.LimitedBuffer:
		if uint64(len(*base)) != bSize {
			return nil, errors.New("invalid packed base size")
		}
		baseBuf = *base
	default:
		baseBuf := make(memio.LimitedBuffer, 0, bSize)
		_, err := baseBuf.ReadFrom(base)
		if err != nil {
			return nil, fmt.Errorf("error reading base object: %w", err)
		} else if len(baseBuf) != cap(baseBuf) {
			return nil, errors.New("invalid packed base size")
		}
	}
	bs = 0x80
	shift = 0
	bSize = 0
	for bs&0x80 != 0 {
		bs = b.ReadUint8()
		bSize |= uint64(bs&0x7f) << shift
		shift += 7
	}
	patched := make(memio.LimitedBuffer, 0, bSize)
	for b.Err == nil {
		instr := b.ReadUint8()
		if instr&0x80 == 0 {
			l := instr & 0x7f
			if l == 0 {
				break
			}
			if _, err := io.CopyN(&patched, z, int64(l)); err != nil {
				return nil, fmt.Errorf("error copying data from patch: %w", err)
			}
		} else {
			var offset, size uint32
			for i := 0; i < 4; i++ {
				if instr&1 == 1 {
					offset |= uint32(b.ReadUint8()) << (i * 8)
				}
				instr >>= 1
			}
			for i := 0; i < 3; i++ {
				if instr&1 == 1 {
					size |= uint32(b.ReadUint8()) << (i * 8)
				}
				instr >>= 1
			}
			if size == 0 {
				size = 0x10000
			}
			if uint32(len(patched))+size > uint32(cap(patched)) {
				return nil, errors.New("patch overwrite")
			}
			patched = append(patched, baseBuf[offset:offset+size]...)
		}
	}
	if b.Err != nil {
		return nil, fmt.Errorf("error reading patch: %w", err)
	}
	if len(patched) != cap(patched) {
		return nil, errors.New("failed to read complete patched object")
	}
	return &patched, nil
}

func (r *Repo) getObject(id string, want int) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(r.path, "objects", id[:2], id[2:]))
	if os.IsNotExist(err) {
		r.loadPacks.Do(r.loadPacksData)
		if r.packsErr != nil {
			err = r.packsErr
		} else if p, ok := r.packObjects[id]; ok {
			return r.readPackOffset(p.pack, p.offset, want)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("error opening object file (%s): %w", id, err)
	}
	close := true
	defer func() {
		if close {
			f.Close()
		}
	}()
	z, err := zlib.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("error decompressing object file (%s): %s", id, err)
	}
	header := objectHeaders[want]
	buf := bufPool.Get().(*[21]byte)
	defer bufPool.Put(buf)
	if _, err := io.ReadFull(z, buf[:len(header)]); err != nil {
		return nil, fmt.Errorf("error reading object header: %w", err)
	}
	if string(buf[:len(header)]) != header {
		return nil, errors.New("wrong type")
	}
	size := false
	for n := range buf {
		if _, err := z.Read(buf[n : n+1]); err != nil {
			return nil, fmt.Errorf("error reading object size: %w", err)
		}
		if buf[n] == 0 {
			size = true
			break
		} else if buf[n] < '0' || buf[n] > '9' {
			return nil, errors.New("invalid object size")
		}
	}
	if !size {
		return nil, errors.New("invalid object size")
	}
	close = false
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
	r.cacheMu.RLock()
	co, ok := r.cache[id]
	r.cacheMu.RUnlock()
	if ok {
		if c, ok := co.(*Commit); ok {
			return c, nil
		}
		return nil, errors.New("wrong type")
	}
	o, err := r.getObject(id, ObjectCommit)
	if err != nil {
		return nil, fmt.Errorf("error while opening commit object: %w", err)
	}
	var buf []byte
	if m, ok := o.(*memio.LimitedBuffer); ok {
		buf = *m
	} else {
		buf, err = io.ReadAll(o)
		o.Close()
		if err != nil {
			return nil, fmt.Errorf("error reading commit: %w", err)
		}
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
	r.cacheMu.Lock()
	r.cache[id] = c
	r.cacheMu.Unlock()
	return c, nil
}

type Tree map[string]string

func (r *Repo) GetTree(id string) (Tree, error) {
	r.cacheMu.RLock()
	co, ok := r.cache[id]
	r.cacheMu.RUnlock()
	if ok {
		if t, ok := co.(Tree); ok {
			return t, nil
		}
		return nil, errors.New("wrong type")
	}
	o, err := r.getObject(id, ObjectTree)
	if err != nil {
		return nil, fmt.Errorf("error while opening tree object: %w", err)
	}
	var buf []byte
	if m, ok := o.(*memio.LimitedBuffer); ok {
		buf = *m
	} else {
		buf, err = io.ReadAll(o)
		o.Close()
		if err != nil {
			return nil, err
		}
	}
	files := make(Tree)
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
		files[name] = fmt.Sprintf("%x", buf[:20])
		buf = buf[20:]
	}
	r.cacheMu.Lock()
	r.cache[id] = files
	r.cacheMu.Unlock()
	return files, nil
}

func (r *Repo) GetBlob(id string) (io.ReadCloser, error) {
	b, err := r.getObject(id, ObjectBlob)
	if err != nil {
		return nil, fmt.Errorf("error reading blob: %w")
	}
	return b, nil
}

func checkSHA(sha []byte) string {
	for _, c := range sha {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return ""
		}
	}
	return string(sha)
}
