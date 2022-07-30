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
		switch c := t.ExceptRun("\"'`/"); c {
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
		default:
			return parser.Token{
				Data: t.Get(),
			}, (*parser.Tokeniser).Done
		}
	}
}

func commentsMultilineQuoted(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	if t.ExceptRun("`") == '`' {
		t.Except("")
		return commentsPlain(t)
	}
	return t.Error()
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
	for {
		if t.ExceptRun("*") == '*' {
			if t.Accept("/") {
				return parser.Token{
					Type: TokenComment,
					Data: t.Get(),
				}, commentsPlain
			}
		} else {
			return t.Error()
		}
	}
}

func highlightComments(file *File, w io.Writer, r io.Reader) (int64, error) {
	return prettify(file, w, r, commentsPlain)
}
