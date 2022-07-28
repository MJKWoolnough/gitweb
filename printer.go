package main

import (
	"io"

	"vimagination.zapto.org/memio"
	"vimagination.zapto.org/parser"
	"vimagination.zapto.org/rwcount"
)

const (
	TokenUnknown parser.TokenType = iota
	TokenNewLine
	TokenComment
)

type Tokens struct {
	TokenUnknown, TokenNewLine, TokenComment parser.TokenType
}

var tokens = Tokens{
	TokenUnknown: TokenUnknown,
	TokenNewLine: TokenNewLine,
	TokenComment: TokenComment,
}

func handleTemplate(file *File, w io.Writer, ch <-chan parser.Phrase, err chan<- error) {
	err <- config.prettyTemplate.Execute(w, struct {
		*File
		Lines <-chan parser.Phrase
		*Tokens
	}{
		File:   file,
		Lines:  ch,
		Tokens: &tokens,
	})
}

func prettify(file *File, w io.Writer, r io.Reader, tf parser.TokenFunc) (int64, error) {
	c := make(chan parser.Phrase)
	e := make(chan error)
	go handleTemplate(file, w, c, e)
	var (
		rw rwcount.Reader
		tk parser.Parser
	)
	if lr, ok := r.(*memio.LimitedBuffer); ok {
		rw.Count = int64(len(*lr))
		tk = parser.New(parser.NewByteTokeniser(*lr))
	} else {
		rw.Reader = r
		tk = parser.New(parser.NewReaderTokeniser(&rw))
	}
	tk.TokeniserState(tf)
	tk.PhraserState(lines)
	for {
		line, err := tk.GetPhrase()
		if err != nil {
			return rw.Count, err
		}
		select {
		case c <- line:
		case err := <-e:
			return rw.Count, err
		}
	}
	return rw.Count, nil
}

func lines(p *parser.Parser) (parser.Phrase, parser.PhraseFunc) {
	for {
		switch p.ExceptRun(TokenNewLine) {
		case TokenNewLine:
		case parser.TokenDone:
			return parser.Phrase{
				Data: p.Get(),
			}, (*parser.Parser).Done
		}
	}
}
