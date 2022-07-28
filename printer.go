package main

import (
	"io"

	"vimagination.zapto.org/parser"
	"vimagination.zapto.org/rwcount"
)

const (
	TokenUnknown parser.TokenType = iota
	TokenNewLine
)

type Tokens struct {
	TokenUnknown, TokenNewLine parser.TokenType
}

var tokens = Tokens{
	TokenUnknown: TokenUnknown,
	TokenNewLine: TokenNewLine,
}

type Token struct {
	Type int
	Data string
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
	rw := rwcount.Reader{Reader: r}
	tk := parser.New(parser.NewReaderTokeniser(&rw))
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
