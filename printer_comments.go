package main

import (
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
			return parser.Token{Data: t.Get()}, commentsMultilineQuoted
		case '/':
			return parser.Token{Data: t.Get()}, comments
		default:
			return commentsOutputRest(t)
		}
	}
}

func commentsMultilineQuoted(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	if t.ExceptRun("`") == '`' {
		t.Except("")

		return commentsPlain(t)
	}

	return commentsOutputRest(t)
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
			t.Except("")

			if t.Accept("/") {
				return parser.Token{
					Type: TokenComment,
					Data: t.Get(),
				}, commentsPlain
			}
		} else {
			return commentsOutputRest(t)
		}
	}
}

func commentsOutputRest(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	t.ExceptRun("")

	return parser.Token{
		Data: t.Get(),
	}, (*parser.Tokeniser).Done
}
