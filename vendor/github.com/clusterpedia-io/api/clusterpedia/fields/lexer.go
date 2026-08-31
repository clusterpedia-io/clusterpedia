/*
	Copy from
		https://github.com/kubernetes/kubernetes/blob/f5be5052e3d0808abb904aebd3218fe4a5c2dd82/staging/src/k8s.io/apimachinery/pkg/labels/selector.go#L452-L588
*/

package fields

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

// string2token contains the mapping between lexer Token and token literal
// (except IdentifierToken, EndOfStringToken and ErrorToken since it makes no sense)
var string2token = map[string]labels.Token{
	")":     labels.ClosedParToken,
	",":     labels.CommaToken,
	"!":     labels.DoesNotExistToken,
	"==":    labels.DoubleEqualsToken,
	"=":     labels.EqualsToken,
	">":     labels.GreaterThanToken,
	"in":    labels.InToken,
	"<":     labels.LessThanToken,
	"!=":    labels.NotEqualsToken,
	"notin": labels.NotInToken,
	"(":     labels.OpenParToken,
}

// ScannedItem contains the Token and the literal produced by the lexer.
type ScannedItem struct {
	tok     labels.Token
	literal string
}

// isWhitespace returns true if the rune is a space, tab, or newline.
func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n'
}

// isSpecialSymbol detects if the character ch can be an operator
func isSpecialSymbol(ch byte) bool {
	switch ch {
	case '=', '!', '(', ')', ',', '>', '<':
		return true
	}
	return false
}

// Lexer represents the Lexer struct for label selector.
// It contains necessary informationt to tokenize the input string
type Lexer struct {
	// s stores the string to be tokenized
	s string
	// pos is the position currently tokenized
	pos int
}

// read returns the character currently lexed
// increment the position and check the buffer overflow
func (l *Lexer) read() (b byte) {
	b = 0
	if l.pos < len(l.s) {
		b = l.s[l.pos]
		l.pos++
	}
	return b
}

// unread 'undoes' the last read character
func (l *Lexer) unread() {
	l.pos--
}

// scanIDOrKeyword scans string to recognize literal token (for example 'in') or an identifier.
func (l *Lexer) scanIDOrKeyword() (tok labels.Token, lit string) {
	var buffer []byte
IdentifierLoop:
	for {
		switch ch := l.read(); {
		case ch == 0:
			break IdentifierLoop
		case isSpecialSymbol(ch) || isWhitespace(ch):
			l.unread()
			break IdentifierLoop
		default:
			buffer = append(buffer, ch)
		}
	}
	s := string(buffer)
	if val, ok := string2token[s]; ok { // is a literal token?
		return val, s
	}
	return labels.IdentifierToken, s // otherwise is an identifier
}

// scanSpecialSymbol scans string starting with special symbol.
// special symbol identify non literal operators. "!=", "==", "="
func (l *Lexer) scanSpecialSymbol() (labels.Token, string) {
	lastScannedItem := ScannedItem{}
	var buffer []byte
SpecialSymbolLoop:
	for {
		switch ch := l.read(); {
		case ch == 0:
			break SpecialSymbolLoop
		case isSpecialSymbol(ch):
			buffer = append(buffer, ch)
			if token, ok := string2token[string(buffer)]; ok {
				lastScannedItem = ScannedItem{tok: token, literal: string(buffer)}
			} else if lastScannedItem.tok != 0 {
				l.unread()
				break SpecialSymbolLoop
			}
		default:
			l.unread()
			break SpecialSymbolLoop
		}
	}
	if lastScannedItem.tok == 0 {
		return labels.ErrorToken, fmt.Sprintf("error expected: keyword found '%s'", buffer)
	}
	return lastScannedItem.tok, lastScannedItem.literal
}

// skipWhiteSpaces consumes all blank characters
// returning the first non blank character
func (l *Lexer) skipWhiteSpaces(ch byte) byte {
	for {
		if !isWhitespace(ch) {
			return ch
		}
		ch = l.read()
	}
}

// Lex returns a pair of Token and the literal
// literal is meaningfull only for IdentifierToken token
func (l *Lexer) Lex() (tok labels.Token, lit string) {
	switch ch := l.skipWhiteSpaces(l.read()); {
	case ch == 0:
		return labels.EndOfStringToken, ""
	case isSpecialSymbol(ch):
		l.unread()
		return l.scanSpecialSymbol()
	default:
		l.unread()
		return l.scanIDOrKeyword()
	}
}
