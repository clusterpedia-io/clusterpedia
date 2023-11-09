package utils

import (
	"strconv"
)

func ParseInt642Str(crv int64) string {
	return strconv.FormatInt(crv, 10)
}

func IsEqual(crvStr1 string, crvStr2 string) bool {
	return crvStr1 == crvStr2
}
