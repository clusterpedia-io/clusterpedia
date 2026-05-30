/*
	Reference from
		https://github.com/kubernetes/kubernetes/blob/f5be5052e3d0808abb904aebd3218fe4a5c2dd82/staging/src/k8s.io/apimachinery/pkg/labels/selector.go#L590

*/

package fields

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	unaryOperators = []string{
		string(selection.Exists), string(selection.DoesNotExist),
	}
	binaryOperators = []string{
		string(selection.In), string(selection.NotIn),
		string(selection.Equals), string(selection.DoubleEquals), string(selection.NotEquals),
		string(selection.GreaterThan), string(selection.LessThan),
	}
	validRequirementOperators = append(binaryOperators, unaryOperators...)
)

type Parser struct {
	l            *Lexer
	scannedItems []ScannedItem
	position     int
}

// ParserContext represents context during parsing:
// some literal for example 'in' and 'notin' can be
// recognized as operator for example 'x in (a)' but
// it can be recognized as value for example 'value in (in)'
type ParserContext int

const (
	// KeyAndOperator represents key and operator
	KeyAndOperator ParserContext = iota
	// Values represents values
	Values
)

func (p *Parser) lookahead(context ParserContext) (labels.Token, string) {
	tok, lit := p.scannedItems[p.position].tok, p.scannedItems[p.position].literal
	if context == Values {
		switch tok {
		case labels.InToken, labels.NotInToken:
			tok = labels.IdentifierToken
		}
	}
	return tok, lit
}

func (p *Parser) consume(context ParserContext) (labels.Token, string) {
	p.position++
	tok, lit := p.scannedItems[p.position-1].tok, p.scannedItems[p.position-1].literal
	if context == Values {
		switch tok {
		case labels.InToken, labels.NotInToken:
			tok = labels.IdentifierToken
		}
	}
	return tok, lit
}

func (p *Parser) scan() {
	for {
		token, literal := p.l.Lex()
		p.scannedItems = append(p.scannedItems, ScannedItem{token, literal})
		if token == labels.EndOfStringToken {
			break
		}
	}
}

func (p *Parser) parse() ([]Requirement, error) {
	p.scan()

	var requirements []Requirement
	for {
		tok, lit := p.lookahead(Values)
		switch tok {
		case labels.IdentifierToken, labels.DoesNotExistToken:
			r, err := p.parseRequirement()
			if err != nil {
				return nil, fmt.Errorf("unable to parse requirement: %v", err)
			}
			requirements = append(requirements, *r)
			t, l := p.consume(Values)
			switch t {
			case labels.EndOfStringToken:
				return requirements, nil
			case labels.CommaToken:
				t2, l2 := p.lookahead(Values)
				if t2 != labels.IdentifierToken && t2 != labels.DoesNotExistToken {
					return nil, fmt.Errorf("found %q, expected: identifier after ','", l2)
				}
			default:
				return nil, fmt.Errorf("found %q, expected: ',' or 'end of string'", l)
			}
		case labels.EndOfStringToken:
			return requirements, nil
		default:
			return nil, fmt.Errorf("found %q, expected: !, identifier, or ''end of string", lit)
		}
	}
}

func (p *Parser) parseRequirement() (*Requirement, error) {
	key, operator, err := p.parseKeyAndInferOperator()
	if err != nil {
		return nil, err
	}
	if operator == selection.Exists || operator == selection.DoesNotExist {
		return NewRequirement(key, operator, []string{})
	}

	operator, err = p.parseOperator()
	if err != nil {
		return nil, err
	}

	var values sets.String
	switch operator {
	case selection.In, selection.NotIn:
		values, err = p.parseValues()
	case selection.Equals, selection.DoubleEquals, selection.NotEquals, selection.GreaterThan, selection.LessThan:
		values, err = p.parseExactValue()
	}
	if err != nil {
		return nil, err
	}
	return NewRequirement(key, operator, values.List())
}

func (p *Parser) parseKeyAndInferOperator() (string, selection.Operator, error) {
	var operator selection.Operator
	tok, literal := p.consume(Values)
	if tok == labels.DoesNotExistToken {
		operator = selection.DoesNotExist
		tok, literal = p.consume(Values)
	}
	if tok != labels.IdentifierToken {
		return "", "", fmt.Errorf("found %q, expected: identifier", literal)
	}

	if t, _ := p.lookahead(Values); t == labels.EndOfStringToken || t == labels.CommaToken {
		if operator != selection.DoesNotExist {
			operator = selection.Exists
		}
	}
	return literal, operator, nil
}

func (p *Parser) parseOperator() (op selection.Operator, err error) {
	tok, lit := p.consume(KeyAndOperator)
	switch tok {
	// DoesNotExistToken shouldn't be here because it's a unary operator, not a binary operator
	case labels.InToken:
		op = selection.In
	case labels.EqualsToken:
		op = selection.Equals
	case labels.DoubleEqualsToken:
		op = selection.DoubleEquals
	case labels.GreaterThanToken:
		op = selection.GreaterThan
	case labels.LessThanToken:
		op = selection.LessThan
	case labels.NotInToken:
		op = selection.NotIn
	case labels.NotEqualsToken:
		op = selection.NotEquals
	default:
		return "", fmt.Errorf("found '%s', expected: %v", lit, strings.Join(binaryOperators, ", "))
	}
	return op, nil
}

func (p *Parser) parseValues() (sets.String, error) {
	tok, lit := p.consume(Values)
	if tok != labels.OpenParToken {
		return nil, fmt.Errorf("found %q, expcted:'('", lit)
	}

	tok, lit = p.lookahead(Values)
	switch tok {
	case labels.IdentifierToken, labels.CommaToken:
		s, err := p.parseIdentifiersList()
		if err != nil {
			return s, err
		}
		if tok, _ = p.consume(Values); tok != labels.ClosedParToken {
			return nil, fmt.Errorf("found '%s', expectedd: ')'", lit)
		}
		return s, nil
	case labels.ClosedParToken:
		p.consume(Values)
		return sets.NewString(""), nil
	default:
		return nil, fmt.Errorf("found %q, expected: ',', ')' or identifier", lit)
	}
}

func (p *Parser) parseIdentifiersList() (sets.String, error) {
	s := sets.NewString()
	for {
		tok, lit := p.consume(Values)
		switch tok {
		case labels.IdentifierToken:
			s.Insert(lit)
			tok2, lit2 := p.lookahead(Values)
			switch tok2 {
			case labels.CommaToken:
				continue
			case labels.ClosedParToken:
				return s, nil
			default:
				return nil, fmt.Errorf("found %q, expected: ',' or ')'", lit2)
			}
		case labels.CommaToken:
			if s.Len() == 0 {
				s.Insert("") // to handle '(,'
			}
			tok2, _ := p.lookahead(Values)
			if tok2 == labels.ClosedParToken {
				s.Insert("") // to handle ',)' Double "" removed by StringSet
				return s, nil
			}
			if tok2 == labels.CommaToken {
				p.consume(Values)
				s.Insert("") // to handle ,, Double "" removed by StringSet
			}
		default:
			return s, fmt.Errorf("found %q, expected: ',',or identifier", lit)
		}
	}
}

func (p *Parser) parseExactValue() (sets.String, error) {
	s := sets.NewString()
	tok, _ := p.lookahead(Values)
	if tok == labels.EndOfStringToken || tok == labels.CommaToken {
		s.Insert("")
		return s, nil
	}
	tok, lit := p.consume(Values)
	if tok != labels.IdentifierToken {
		return nil, fmt.Errorf("found %q, expected: identifier", lit)
	}
	s.Insert(lit)
	return s, nil
}

// safeSort sorts input strings without modification
func safeSort(in []string) []string {
	if sort.StringsAreSorted(in) {
		return in
	}
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}
