package fields

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type Requirements []Requirement

// Selector represents a label selector
type Selector interface {
	Empty() bool

	String() string

	// Add adds requirements to the Selector
	Add(r ...Requirement) Selector

	Requirements() (requirements Requirements, selectable bool)

	// Make a deep copy of the selector.
	DeepCopySelector() Selector
}

type internalSelector []Requirement

func (s internalSelector) Empty() bool { return len(s) == 0 }

func (s internalSelector) Requirements() (Requirements, bool) { return Requirements(s), true }

func (s internalSelector) DeepCopy() internalSelector {
	if s == nil {
		return nil
	}

	// The `field.Path` struct is included in the `Field`,
	// so DeepCopy becomes more complicated, and using `Parse`
	// simplifies the logic.
	result, _ := Parse(s.String())
	return result.(internalSelector)
}

func (s internalSelector) DeepCopySelector() Selector {
	return s.DeepCopy()
}

func (s internalSelector) Add(reqs ...Requirement) Selector {
	ret := make(internalSelector, 0, len(s)+len(reqs))
	ret = append(ret, s...)
	ret = append(ret, reqs...)
	sort.Sort(ByKey(ret))
	return ret
}

func (s internalSelector) String() string {
	var reqs []string
	for ix := range s {
		reqs = append(reqs, s[ix].String())
	}
	return strings.Join(reqs, ",")
}

// ByKey sorts requirements by key to obtain deterministic parser
type ByKey []Requirement

func (rs ByKey) Len() int { return len(rs) }

func (rs ByKey) Swap(i, j int) { rs[i], rs[j] = rs[j], rs[i] }

func (rs ByKey) Less(i, j int) bool { return rs[i].key < rs[j].key }

type Requirement struct {
	key string

	fields    []Field
	operator  selection.Operator
	strValues []string
}

func NewRequirement(key string, op selection.Operator, vals []string) (*Requirement, error) {
	fields, err := parseFields(key, nil)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errors.New("fields is empty")
	}

	var allErrs field.ErrorList
	for _, field := range fields {
		if err := field.Validate(); err != nil {
			allErrs = append(allErrs, err)
		}
	}

	lastField := fields[len(fields)-1]
	path := lastField.Path()
	if lastField.IsList() {
		allErrs = append(allErrs, field.Invalid(path, lastField.Name(), "last field could not be list"))
	}

	valuePath := path.Child("values")
	switch op {
	case selection.In, selection.NotIn:
		if len(vals) == 0 {
			allErrs = append(allErrs, field.Invalid(valuePath, vals, "for 'in', 'notin' operators, values set can't be empty"))
		}
	case selection.Equals, selection.DoubleEquals, selection.NotEquals:
		if len(vals) != 1 {
			allErrs = append(allErrs, field.Invalid(valuePath, vals, "exact-match compatibility requires one single value"))
		}
	case selection.Exists, selection.DoesNotExist:
		if len(vals) != 0 {
			allErrs = append(allErrs, field.Invalid(valuePath, vals, "values set must be empty for exists and does not exist"))
		}
	case selection.GreaterThan, selection.LessThan:
		if len(vals) != 1 {
			allErrs = append(allErrs, field.Invalid(valuePath, vals, "for 'Gt', 'Lt' operators, exactly one value is required"))
		}
		for i := range vals {
			if _, err := strconv.ParseInt(vals[i], 10, 64); err != nil {
				allErrs = append(allErrs, field.Invalid(valuePath.Index(i), vals[i], "for 'Gt', 'Lt' operators, the value must be an integer"))
			}
		}
	default:
		allErrs = append(allErrs, field.NotSupported(path.Child("operator"), op, validRequirementOperators))
	}

	// values not validate

	return &Requirement{key: key, fields: fields, operator: op, strValues: vals}, allErrs.ToAggregate()
}

func (r *Requirement) Fields() []Field {
	fields := make([]Field, len(r.fields))
	copy(fields, r.fields)
	return fields
}

func (r *Requirement) Operator() selection.Operator {
	return r.operator
}

func (r *Requirement) Values() sets.String {
	ret := sets.String{}
	for i := range r.strValues {
		ret.Insert(r.strValues[i])
	}
	return ret
}

func (r *Requirement) String() string {
	var sb strings.Builder
	sb.Grow(
		// length of r.key
		len(r.key) +
			// length of 'r.operator' + 2 spaces for the worst case ('in' and 'notin')
			len(r.operator) + 2 +
			// length of 'r.strValues' slice times. Heuristically 5 chars per word
			+5*len(r.strValues))
	if r.operator == selection.DoesNotExist {
		sb.WriteString("!")
	}
	sb.WriteString(r.key)

	switch r.operator {
	case selection.Equals:
		sb.WriteString("=")
	case selection.DoubleEquals:
		sb.WriteString("==")
	case selection.NotEquals:
		sb.WriteString("!=")
	case selection.In:
		sb.WriteString(" in ")
	case selection.NotIn:
		sb.WriteString(" notin ")
	case selection.GreaterThan:
		sb.WriteString(">")
	case selection.LessThan:
		sb.WriteString("<")
	case selection.Exists, selection.DoesNotExist:
		return sb.String()
	}

	switch r.operator {
	case selection.In, selection.NotIn:
		sb.WriteString("(")
	}
	if len(r.strValues) == 1 {
		sb.WriteString(r.strValues[0])
	} else { // only > 1 since == 0 prohibited by NewRequirement
		// normalizes value order on output, without mutating the in-memory selector representation
		// also avoids normalization when it is not required, and ensures we do not mutate shared data
		sb.WriteString(strings.Join(safeSort(r.strValues), ","))
	}

	switch r.operator {
	case selection.In, selection.NotIn:
		sb.WriteString(")")
	}
	return sb.String()
}

type Field struct {
	path *field.Path

	name   string
	isList bool
	index  int
}

func NewField(parentPath *field.Path, name string) Field {
	var path *field.Path
	if parentPath == nil {
		path = field.NewPath(name)
	} else {
		path = parentPath.Child(name)
	}

	return Field{path: path, name: name}
}

func (f *Field) setListIndex(index int) {
	f.path = f.path.Index(index)
	f.isList = true
	f.index = index
}

func (f *Field) Name() string {
	return f.name
}

func (f *Field) IsList() bool {
	return f.isList
}

func (f *Field) GetListIndex() (int, bool) {
	return f.index, f.isList
}

func (f *Field) Path() *field.Path {
	return f.path
}

func (f *Field) Validate() *field.Error {
	if errs := validation.IsQualifiedName(f.name); len(errs) != 0 {
		return field.Invalid(f.path, f.name, strings.Join(errs, "; "))
	}
	return nil
}

func parseFields(key string, fields []Field) ([]Field, error) {
	if len(key) == 0 {
		return fields, nil
	}

	if key[0] == '.' {
		if len(key) == 1 {
			return nil, errors.New("empty field after '.'")
		}

		key = key[1:]
	}

	var parentPath *field.Path
	if len(fields) != 0 {
		parentPath = fields[len(fields)-1].Path()
	}

	if key[0] == '[' {
		rightIndex := strings.IndexByte(key, ']')
		switch {
		case rightIndex == -1:
			return nil, errors.New("not found ']'")

		// handle 'field[]'
		case rightIndex == 1:
			if len(fields) == 0 {
				return nil, errors.New("empty [], not found list field")
			}
			fields[len(fields)-1].isList = true

		// handle `lastfield['field']`
		case key[1] == '\'' || key[1] == '"':
			inSquKey := key[1:rightIndex]

			wrap, inSquKey := inSquKey[0], inSquKey[1:]
			rightSquIndex := strings.IndexByte(inSquKey, wrap)
			switch rightSquIndex {
			case -1:
				return nil, fmt.Errorf("not found right '%c'", wrap)
			case 1:
				return nil, fmt.Errorf("empty field %c%c", wrap, wrap)
			case len(inSquKey) - 1:
				fields = append(fields, NewField(parentPath, inSquKey[0:rightSquIndex]))
			default:
				return nil, fmt.Errorf("invalid field ['%s]", inSquKey)
			}

		// handle 'field[0]'
		default:
			if len(fields) == 0 {
				return nil, errors.New("[<index>], not found list field")
			}
			lastField := &fields[len(fields)-1]

			indexStr := key[1:rightIndex]
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return nil, fmt.Errorf("%s[<index>] list index invalid. if %s is a field, please use ['%s'] or .'%s'", lastField.Path(), indexStr, indexStr, indexStr)
			}

			lastField.setListIndex(index)
		}
		return parseFields(key[rightIndex+1:], fields)
	}

	if key[0] == '\'' || key[0] == '"' {
		wrap := key[0]
		if len(key) == 1 {
			return nil, fmt.Errorf("not found right '%c'", wrap)
		}

		key = key[1:]
		rightIndex := strings.IndexByte(key, wrap)
		if rightIndex == -1 {
			return nil, fmt.Errorf("not found right '%c'", wrap)
		}
		if rightIndex == 1 {
			return nil, fmt.Errorf("empty field %c%c", wrap, wrap)
		}

		fields = append(fields, NewField(parentPath, key[0:rightIndex]))
		return parseFields(key[rightIndex+1:], fields)
	}

	rightIndex := strings.IndexAny(key, ".[")
	if rightIndex == -1 {
		fields = append(fields, NewField(parentPath, key))
		return fields, nil
	}

	fields = append(fields, NewField(parentPath, key[:rightIndex]))
	return parseFields(key[rightIndex:], fields)
}

func Parse(selector string) (Selector, error) {
	p := &Parser{l: &Lexer{s: selector, pos: 0}}
	items, err := p.parse()
	if err != nil {
		return nil, err
	}
	sort.Sort(ByKey(items))
	return internalSelector(items), nil
}
