package fields

import (
	"testing"
)

func TestParseFields(t *testing.T) {
	testGoodStrings := []string{
		"metadata.name",
		".metadata.name",
		".metadata.annotations.'test.io'",
		".metadata.annotations['test.io']",
		".field1['prefix.field2'].field3",
		".field1['prefix.field2']['prefix.field3']",
		".spec.containers[].name",
		".spec.containers[0].name",
		".field1['prefix.listed'][0].field2",
	}
	testBadStrings := []string{
		"[]",
		"[0]",
		".metadata.annotations[test.io]",
		".metadata.annotations['test.io'.go]",
	}

	for _, test := range testGoodStrings {
		_, err := parseFields(test, nil)
		if err != nil {
			t.Errorf("%v: error %v\n", test, err)
		}
	}

	for _, test := range testBadStrings {
		_, err := parseFields(test, nil)
		if err == nil {
			t.Errorf("%v: did not get expected error\n", test)
		}
	}
}

func TestSelectorParse(t *testing.T) {
	testGoodStrings := []string{
		"",
		".metadata.annotations['test.io'] in (value1,value2),.spec.replica=3",
		"metadata.annotations.'test.io'==value1",
		"!metadata.annotation['test.io']",
		"spec.containers[].name!=container1",
		".spec.containers[].name==container1",
		".spec.containers[1].name in (container1,container2)",
	}
	testBadStrings := []string{
		".metadata.annotations[test.io] in (value1, value2)",
		".metadata.annotations['test'io'] in (value1, value2)",
		"spec.containers[]==something",
	}

	for _, test := range testGoodStrings {
		selector, err := Parse(test)
		if err != nil {
			t.Errorf("%v: error %v\n", test, err)
		}
		if test != selector.String() {
			t.Errorf("%v restring gave: %v \n", test, selector.String())
		}
	}
	for _, test := range testBadStrings {
		_, err := Parse(test)
		if err == nil {
			t.Errorf("%v: did not get expected error\n", test)
		}
	}
}
