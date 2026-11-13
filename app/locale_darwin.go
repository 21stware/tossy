package main

import (
	"os/exec"
	"regexp"
)

var appleLangRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*`)

// systemLocales are the user's preferred languages, in order: System Settings' list (an app
// started from the Finder has no LANG), then the environment.
func systemLocales() []string {
	var out []string
	if b, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		out = appleLangRe.FindAllString(string(b), -1) // ( "zh-Hant-TW", "en-US" )
	}
	return append(out, envLocales()...)
}
