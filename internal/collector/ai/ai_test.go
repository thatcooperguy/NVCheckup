package ai

import "testing"

const cudnn9Header = `/*
 * Copyright 2014-2024 NVIDIA Corporation.
 */
#ifndef CUDNN_VERSION_H_
#define CUDNN_VERSION_H_

#define CUDNN_MAJOR 9
#define CUDNN_MINOR 1
#define CUDNN_PATCHLEVEL 0

#define CUDNN_VERSION (CUDNN_MAJOR * 10000 + CUDNN_MINOR * 100 + CUDNN_PATCHLEVEL)

/* cannot use constexpr here since this is a C-only file */
#define CUDNN_MAX_SM_MAJOR_NUMBER 9
#define CUDNN_MAX_SM_MINOR_NUMBER 0
#define CUDNN_MAX_DEVICE_VERSION (CUDNN_MAX_SM_MAJOR_NUMBER * 100) + (CUDNN_MAX_SM_MINOR_NUMBER * 10)

#endif /* CUDNN_VERSION_H */
`

func TestParseCudnnHeader(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"cudnn 9 header", cudnn9Header, "9.1.0"},
		{"legacy cudnn.h", "#define CUDNN_MAJOR 7\n#define CUDNN_MINOR 6\n#define CUDNN_PATCHLEVEL 5\n", "7.6.5"},
		{"indented define with tab", "  #\tdefine CUDNN_MAJOR 8\n#define CUDNN_MINOR 9\n", "8.9"},
		{"major only", "#define CUDNN_MAJOR 8\n", "8"},
		{"crlf line endings", "#define CUDNN_MAJOR 8\r\n#define CUDNN_MINOR 2\r\n#define CUDNN_PATCHLEVEL 4\r\n", "8.2.4"},
		{"expression macro does not count as a define", "#define CUDNN_VERSION (CUDNN_MAJOR * 1000)\n", ""},
		{"empty", "", ""},
		{"unrelated header", "#define FOO 1\n", ""},
	}
	for _, c := range cases {
		if got := parseCudnnHeader(c.content); got != c.want {
			t.Errorf("%s: parseCudnnHeader = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLastLine(t *testing.T) {
	tb := "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\nModuleNotFoundError: No module named 'torch'\n\n"
	if got := lastLine(tb); got != "ModuleNotFoundError: No module named 'torch'" {
		t.Errorf("lastLine = %q", got)
	}
	if got := lastLine("  \n"); got != "" {
		t.Errorf("lastLine(blank) = %q", got)
	}
}
