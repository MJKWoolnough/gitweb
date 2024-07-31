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
					t.Next()
					t.Next()
				case c:
					t.Next()

					break QuoteLoop
				}
			}
		case '`':
			return t.Return(TokenUnknown, commentsMultilineQuoted)
		case '/':
			return t.Return(TokenUnknown, comments)
		default:
			return commentsOutputRest(t)
		}
	}
}

func commentsMultilineQuoted(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	if t.ExceptRun("`") == '`' {
		t.Next()

		return commentsPlain(t)
	}

	return commentsOutputRest(t)
}

func comments(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	t.Next()

	if t.Accept("/") {
		t.ExceptRun("\n")

		return t.Return(TokenComment, commentsPlain)
	} else if t.Accept("*") {
		return commentsMultiline(t)
	}

	return commentsPlain(t)
}

func commentsMultiline(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	for {
		if t.ExceptRun("*") == '*' {
			t.Next()

			if t.Accept("/") {
				return t.Return(TokenComment, commentsPlain)
			}
		} else {
			return commentsOutputRest(t)
		}
	}
}

func commentsOutputRest(t *parser.Tokeniser) (parser.Token, parser.TokenFunc) {
	t.ExceptRun("")

	return t.Return(TokenUnknown, nil)
}
