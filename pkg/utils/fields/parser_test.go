package fields

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/labels"
)

func TestSafeSort(t *testing.T) {
	tests := []struct {
		name   string
		in     []string
		inCopy []string
		want   []string
	}{
		{
			name:   "nil strings",
			in:     nil,
			inCopy: nil,
			want:   nil,
		},
		{
			name:   "ordered strings",
			in:     []string{"bar", "foo"},
			inCopy: []string{"bar", "foo"},
			want:   []string{"bar", "foo"},
		},
		{
			name:   "unordered strings",
			in:     []string{"foo", "bar"},
			inCopy: []string{"foo", "bar"},
			want:   []string{"bar", "foo"},
		},
		{
			name:   "duplicated strings",
			in:     []string{"foo", "bar", "foo", "bar"},
			inCopy: []string{"foo", "bar", "foo", "bar"},
			want:   []string{"bar", "bar", "foo", "foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeSort(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("safeSort() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.in, tt.inCopy) {
				t.Errorf("after safeSort(), input = %v, want %v", tt.in, tt.inCopy)
			}
		})
	}
}

func TestParserLookahead(t *testing.T) {
	testcases := []struct {
		s string
		t []labels.Token
	}{
		{"key in ( value )", []labels.Token{labels.IdentifierToken, labels.InToken, labels.OpenParToken, labels.IdentifierToken, labels.ClosedParToken, labels.EndOfStringToken}},
		{"key notin ( value )", []labels.Token{labels.IdentifierToken, labels.NotInToken, labels.OpenParToken, labels.IdentifierToken, labels.ClosedParToken, labels.EndOfStringToken}},
		{"key in ( value1, value2 )", []labels.Token{labels.IdentifierToken, labels.InToken, labels.OpenParToken, labels.IdentifierToken, labels.CommaToken, labels.IdentifierToken, labels.ClosedParToken, labels.EndOfStringToken}},
		{"key", []labels.Token{labels.IdentifierToken, labels.EndOfStringToken}},
		{"!key", []labels.Token{labels.DoesNotExistToken, labels.IdentifierToken, labels.EndOfStringToken}},
		{"()", []labels.Token{labels.OpenParToken, labels.ClosedParToken, labels.EndOfStringToken}},
		{"", []labels.Token{labels.EndOfStringToken}},
		{"x in (),y", []labels.Token{labels.IdentifierToken, labels.InToken, labels.OpenParToken, labels.ClosedParToken, labels.CommaToken, labels.IdentifierToken, labels.EndOfStringToken}},
		{"== != (), = notin", []labels.Token{labels.DoubleEqualsToken, labels.NotEqualsToken, labels.OpenParToken, labels.ClosedParToken, labels.CommaToken, labels.EqualsToken, labels.NotInToken, labels.EndOfStringToken}},
		{"key>2", []labels.Token{labels.IdentifierToken, labels.GreaterThanToken, labels.IdentifierToken, labels.EndOfStringToken}},
		{"key<1", []labels.Token{labels.IdentifierToken, labels.LessThanToken, labels.IdentifierToken, labels.EndOfStringToken}},
	}
	for _, v := range testcases {
		p := &Parser{l: &Lexer{s: v.s, pos: 0}, position: 0}
		p.scan()
		if len(p.scannedItems) != len(v.t) {
			t.Errorf("Expected %d items found %d", len(v.t), len(p.scannedItems))
		}
		for {
			token, lit := p.lookahead(KeyAndOperator)

			token2, lit2 := p.consume(KeyAndOperator)
			if token == labels.EndOfStringToken {
				break
			}
			if token != token2 || lit != lit2 {
				t.Errorf("Bad values")
			}
		}
	}
}
