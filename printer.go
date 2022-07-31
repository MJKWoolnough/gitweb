package main

import (
	"io"

	"vimagination.zapto.org/memio"
	"vimagination.zapto.org/parser"
	"vimagination.zapto.org/rwcount"
)

const (
	TokenUnknown parser.TokenType = iota
	TokenComment
)

type Tokens struct {
	Unknown, Comment parser.TokenType
}

var tokens = Tokens{
	Unknown: TokenUnknown,
	Comment: TokenComment,
}

func handleTemplate(file *File, w io.Writer, ch <-chan parser.Token, err chan<- error) {
	err <- config.prettyTemplate.Execute(w, struct {
		*File
		Tokens     <-chan parser.Token
		TokenTypes *Tokens
	}{
		File:       file,
		Tokens:     ch,
		TokenTypes: &tokens,
	})
}

func prettify(file *File, w io.Writer, r io.Reader, tf parser.TokenFunc) (int64, error) {
	if tf == nil {
		return io.Copy(w, r)
	}
	c := make(chan parser.Token)
	e := make(chan error)
	go handleTemplate(file, w, c, e)
	var (
		rw rwcount.Reader
		p  parser.Parser
	)
	if lr, ok := r.(*memio.LimitedBuffer); ok {
		rw.Count = int64(len(*lr))
		p = parser.New(parser.NewByteTokeniser(*lr))
	} else {
		rw.Reader = r
		p = parser.New(parser.NewReaderTokeniser(&rw))
	}
	p.TokeniserState(tf)
	for {
		tk, err := p.GetToken()
		if err != nil {
			close(c)
			<-e
			return rw.Count, err
		}
		if tk.Type == parser.TokenDone {
			close(c)
			break
		}
		select {
		case c <- tk:
		case err := <-e:
			return rw.Count, err
		}
	}
	if err := <-e; err != nil {
		return rw.Count, err
	}
	if rw.Err == io.EOF {
		rw.Err = nil
	}
	return rw.Count, rw.Err
}
