package privacy

import (
	"regexp"
	"strings"
)

var (
	apiKeyRE   = regexp.MustCompile(`(?i)(api[_-]?key|apikey|secret|token|password|credential)[:=]\s*['"]?\S{8,}['"]?`)
	ssnRE      = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	emailRE    = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	urlWithKey = regexp.MustCompile(`https?://[^:]+:[^@]+@`)
	ipRE       = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	jwtRE      = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

type Redactor struct {
	patterns     []*regexp.Regexp
	replace      string
	excludePaths []string
}

func NewRedactor() *Redactor {
	return &Redactor{
		patterns: []*regexp.Regexp{apiKeyRE, ssnRE, jwtRE, urlWithKey},
		replace:  "[REDACTED]",
	}
}

func (r *Redactor) AddPattern(re *regexp.Regexp) {
	r.patterns = append(r.patterns, re)
}

func (r *Redactor) AddExclude(path string) {
	r.excludePaths = append(r.excludePaths, path)
}

func (r *Redactor) Redact(data string) string {
	result := data
	for _, re := range r.patterns {
		result = re.ReplaceAllString(result, r.replace)
	}
	return result
}

func (r *Redactor) RedactOutput(data string) string {
	result := r.Redact(data)
	result = emailRE.ReplaceAllString(result, r.replace)
	result = ipRE.ReplaceAllString(result, r.replace)
	return result
}

func (r *Redactor) ContainsSecrets(data string) bool {
	for _, re := range r.patterns {
		if re.MatchString(data) {
			return true
		}
	}
	return emailRE.MatchString(data)
}

func (r *Redactor) ShouldExclude(path string) bool {
	for _, ex := range r.excludePaths {
		if strings.HasPrefix(path, ex) || strings.Contains(path, ex) {
			return true
		}
	}
	return false
}
