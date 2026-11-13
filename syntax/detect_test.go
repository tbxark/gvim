package syntax

import "testing"

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  string
		want     string
	}{
		{name: "go by extension", filename: "main.go", content: "package main\n", want: "Go"},
		{name: "python by extension", filename: "tool.py", content: "print(1)\n", want: "Python"},
		{name: "markdown by extension", filename: "README.md", content: "# Title\n", want: "markdown"},
		{name: "json by extension", filename: "config.json", content: "{}\n", want: "JSON"},
		{name: "extension beats shebang", filename: "run.py", content: "#!/bin/sh\necho hi\n", want: "Python"},
		{name: "shebang without extension", filename: "deploy", content: "#!/usr/bin/env python3\nprint('x')\n", want: "Python"},
		{name: "shebang with version suffix", filename: "build", content: "#!/usr/local/bin/python3.11\n", want: "Python"},
		{name: "env -S shebang", filename: "x", content: "#!/usr/bin/env -S python3 -u\n", want: "Python"},
		{name: "shell shebang via analyse", filename: "", content: "#!/bin/sh\necho hi\n", want: "Bash"},
		{name: "unified diff with Go content", filename: "", content: "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-package a\n+package b\n", want: "Diff"},
		{name: "plain unified diff", filename: "", content: "--- old.txt\n+++ new.txt\n@@ -1 +1 @@\n-old\n+new\n", want: "Diff"},
		{name: "unknown interpreter falls back", filename: "tool", content: "#!/usr/bin/env zzqx\nhi\n", want: "fallback"},
		{name: "unknown extension falls back", filename: "notes.zzqx", content: "plain words here\n", want: "fallback"},
		{name: "empty name and content falls back", filename: "", content: "", want: "fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.filename, []byte(tt.content)).Config().Name
			if got != tt.want {
				t.Fatalf("Detect(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestDetectNeverNil(t *testing.T) {
	if Detect("x.unknownext", nil) == nil {
		t.Fatal("Detect returned nil")
	}
}
