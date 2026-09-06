// Package java finds, selects and installs Java runtimes.
package java

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseVersion extracts the major release and full version string from the
// output of `java -version`, which is written to stderr in forms such as:
//
//	openjdk version "1.8.0_392"
//	openjdk version "17.0.15" 2025-04-15 LTS
//	java version "21.0.2" 2024-01-16 LTS
//
// Java 8 and earlier use the 1.x numbering, where the major release is the
// second component.
func ParseVersion(out string) (major int, full string, err error) {
	full = extractQuoted(out)
	if full == "" {
		return 0, "", fmt.Errorf("no version string in %q", firstLine(out))
	}

	// Split on the separators used across the two numbering schemes.
	parts := strings.FieldsFunc(full, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '+'
	})
	if len(parts) == 0 {
		return 0, full, fmt.Errorf("cannot parse version %q", full)
	}

	first, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, full, fmt.Errorf("cannot parse version %q", full)
	}

	if first == 1 {
		// 1.8.0_392 → 8
		if len(parts) < 2 {
			return 0, full, fmt.Errorf("cannot parse legacy version %q", full)
		}
		second, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, full, fmt.Errorf("cannot parse legacy version %q", full)
		}
		return second, full, nil
	}

	return first, full, nil
}

// extractQuoted returns the first double-quoted run in s.
func extractQuoted(s string) string {
	start := strings.Index(s, `"`)
	if start < 0 {
		return ""
	}
	rest := s[start+1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ParseVendor makes a rough guess at the distribution from `java -version`
// output. It is for display only; nothing depends on it.
func ParseVendor(out string) string {
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "graalvm"):
		return "GraalVM"
	case strings.Contains(lower, "zulu"):
		return "Zulu"
	case strings.Contains(lower, "temurin"):
		return "Temurin"
	case strings.Contains(lower, "microsoft"):
		return "Microsoft"
	case strings.Contains(lower, "corretto"):
		return "Corretto"
	case strings.Contains(lower, "openjdk"):
		return "OpenJDK"
	case strings.Contains(lower, "java(tm)"), strings.Contains(lower, "hotspot"):
		return "Oracle"
	}
	return ""
}
