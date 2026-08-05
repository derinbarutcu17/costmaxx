package shared

import (
	"regexp"
	"strings"
	"unicode"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
var controlRe = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f]`)

func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func NormalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func CollapseDuplicateLines(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	var prev string
	for _, line := range lines {
		if line != prev {
			result = append(result, line)
		}
		prev = line
	}
	return strings.Join(result, "\n")
}

func IsBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func DetectEncoding(data []byte) string {
	hasNull := false
	for _, b := range data[:min(len(data), 1024)] {
		if b == 0 {
			hasNull = true
			break
		}
	}
	if hasNull {
		return "utf-16"
	}
	return "utf-8"
}

func Preprocess(s string) string {
	s = StripANSI(s)
	s = NormalizeLineEndings(s)
	s = CollapseDuplicateLines(s)
	return s
}

func Printables(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			return r
		}
		return -1
	}, s)
}

func ExtractPaths(s string) []string {
	re := regexp.MustCompile(`[a-zA-Z0-9_\-./]+\.[a-zA-Z]+:\d+(?::\d+)?`)
	return re.FindAllString(s, -1)
}

func ExtractErrors(s string) []string {
	re := regexp.MustCompile(`(?i)(error|exception|failure|failed|fatal|panic):?\s*(.*)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		full := strings.TrimSpace(m[0])
		if len(full) > 0 {
			out = append(out, full)
		}
	}
	return out
}
