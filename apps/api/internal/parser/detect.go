package parser

import (
	"bytes"
	"io"
	"strings"
)

// Detect guesses the format from the first 4 KB of content.
func Detect(content []byte) Format {
	// HTML detection: "<!DOCTYPE", "<html", or "<table"
	head := bytes.ToLower(content[:min(len(content), 4096)])
	if bytes.Contains(head, []byte("<table")) || bytes.Contains(head, []byte("<!doctype html")) {
		return FormatMT5HTML
	}
	// CSV detection: first line contains typical MT5 headers
	firstLine := head
	if idx := bytes.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLineStr := strings.ToLower(string(firstLine))
	if strings.Contains(firstLineStr, "ticket") && strings.Contains(firstLineStr, "symbol") {
		return FormatMT5CSV
	}
	if strings.Contains(firstLineStr, "ticket") || strings.Contains(firstLineStr, "symbol") {
		return FormatGenericCSV
	}
	return FormatUnknown
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ReadAll safely reads up to max bytes from r.
func ReadAll(r io.Reader, max int64) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	lr := io.LimitReader(r, max+1)
	_, err := io.Copy(buf, lr)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}