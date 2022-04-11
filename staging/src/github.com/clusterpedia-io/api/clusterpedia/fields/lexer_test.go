package fields

import (
	"testing"

	"k8s.io/apimachinery/pkg/labels"
)

func TestLexer(t *testing.T) {
	testcases := []struct {
		s string
		t labels.Token
	}{
		{"", labels.EndOfStringToken},
		{",", labels.CommaToken},
		{"notin", labels.NotInToken},
		{"in", labels.InToken},
		{"=", labels.EqualsToken},
		{"==", labels.DoubleEqualsToken},
		{">", labels.GreaterThanToken},
		{"<", labels.LessThanToken},
		//Note that Lex returns the longest valid token found
		{"!", labels.DoesNotExistToken},
		{"!=", labels.NotEqualsToken},
		{"(", labels.OpenParToken},
		{")", labels.ClosedParToken},
		//Non-"special" characters are considered part of an identifier
		{"~", labels.IdentifierToken},
		{"||", labels.IdentifierToken},
	}
	for _, v := range testcases {
		l := &Lexer{s: v.s, pos: 0}
		token, lit := l.Lex()
		if token != v.t {
			t.Errorf("Got %d it should be %d for '%s'", token, v.t, v.s)
		}
		if v.t != labels.ErrorToken && lit != v.s {
			t.Errorf("Got '%s' it should be '%s'", lit, v.s)
		}
	}
}

func min(l, r int) (m int) {
	m = r
	if l < r {
		m = l
	}
	return m
}

func TestLexerSequence(t *testing.T) {
	testcases := []struct {
		s string
		t []labels.Token
	}{
		{"key in ( value )", []labels.Token{labels.IdentifierToken, labels.InToken, labels.OpenParToken, labels.IdentifierToken, labels.ClosedParToken}},
		{"key notin ( value )", []labels.Token{labels.IdentifierToken, labels.NotInToken, labels.OpenParToken, labels.IdentifierToken, labels.ClosedParToken}},
		{"key in ( value1, value2 )", []labels.Token{labels.IdentifierToken, labels.InToken, labels.OpenParToken, labels.IdentifierToken, labels.CommaToken, labels.IdentifierToken, labels.ClosedParToken}},
		{"key", []labels.Token{labels.IdentifierToken}},
		{"!key", []labels.Token{labels.DoesNotExistToken, labels.IdentifierToken}},
		{"()", []labels.Token{labels.OpenParToken, labels.ClosedParToken}},
		{"x in (),y", []labels.Token{labels.IdentifierToken, labels.InToken, labels.OpenParToken, labels.ClosedParToken, labels.CommaToken, labels.IdentifierToken}},
		{"== != (), = notin", []labels.Token{labels.DoubleEqualsToken, labels.NotEqualsToken, labels.OpenParToken, labels.ClosedParToken, labels.CommaToken, labels.EqualsToken, labels.NotInToken}},
		{"key>2", []labels.Token{labels.IdentifierToken, labels.GreaterThanToken, labels.IdentifierToken}},
		{"key<1", []labels.Token{labels.IdentifierToken, labels.LessThanToken, labels.IdentifierToken}},
	}
	for _, v := range testcases {
		var tokens []labels.Token
		l := &Lexer{s: v.s, pos: 0}
		for {
			token, _ := l.Lex()
			if token == labels.EndOfStringToken {
				break
			}
			tokens = append(tokens, token)
		}
		if len(tokens) != len(v.t) {
			t.Errorf("Bad number of tokens for '%s %d, %d", v.s, len(tokens), len(v.t))
		}
		for i := 0; i < min(len(tokens), len(v.t)); i++ {
			if tokens[i] != v.t[i] {
				t.Errorf("Test '%s': Mismatching in token type found '%v' it should be '%v'", v.s, tokens[i], v.t[i])
			}
		}
	}
}
