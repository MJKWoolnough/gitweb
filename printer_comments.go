package main

import (
	"io"

	"vimagination.zapto.org/parser"
)

var (
	commentsQuotedExceptDouble = "\n\\\""
	commentsQuotedExceptSingle = "\n\\'"
)

func commentsPlain(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	for {
		switch c := t.ExceptRun("\"'`/\n"); c {
		case -1:
			return t.Error()
		case '"', '\'':
			er := commentsQuotedExceptSingle
			if c == '"' {
				er = commentsQuotedExceptDouble
			}
		QuoteLoop:
			for {
				switch t.ExceptRun(er) {
				case -1, '\n':
					return t.Error()
				case '\\':
					t.Except("")
					t.Except("")
				case c:
					t.Except("")
					break QuoteLoop
				}
			}
		case '`':
			return parser.Token{
				Data: t.Get(),
			}, commentsMultilineQuoted
		case '/':
			return parser.Token{
				Data: t.Get(),
			}, comments
		case '\n':
			t.Except("")
			return parser.Token{
				Type: TokenNewLine,
				Data: t.Get(),
			}, commentsPlain
		}
	}
}

func commentsMultilineQuoted(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	switch t.ExceptRun("`\n") {
	case '\n':
		t.Except("")
		return parser.Token{
			Data: t.Get(),
		}, commentsMultilineQuoted
	case '`':
		t.Except("")
		return commentsPlain(t)
	default:
		return t.Error()
	}
}

func comments(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	t.Except("")
	if t.Accept("/") {
		t.ExceptRun("\n")
		return parser.Token{
			Type: TokenComment,
			Data: t.Get(),
		}, commentsPlain
	} else if t.Accept("*") {
		return commentsMultiline(t)
	}
	return commentsPlain(t)
}

func commentsMultiline(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	c := t.ExceptRun("`\n")
	switch c {
	case '`':
		t.Except("")
		return parser.Token{
			Type: TokenComment,
			Data: t.Get(),
		}, commentsPlain
	case '\n':
		return parser.Token{
			Type: TokenComment,
			Data: t.Get(),
		}, commentsNewLineInComment
	default:
		return t.Error()
	}
}

func commentsNewLineInComment(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	t.Except("")
	return parser.Token{
		Type: TokenNewLine,
		Data: t.Get(),
	}, commentsMultiline
}

func highlightComments(file *File, w io.Writer, r io.Reader) (int64, error) {
	return prettify(file, w, r, commentsPlain)
}
