package authhttp

import (
	"net/url"
	"strconv"
)

// urlQueryEscape escapes a string for safe inclusion in a URL query value.
func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// i64ToStr converts int64 to decimal string.
func i64ToStr(n int64) string { return strconv.FormatInt(n, 10) }