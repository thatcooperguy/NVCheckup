package ai

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

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

func TestSelectPython(t *testing.T) {
	// Store stub "python3" exists but fails the probe; "python" is absent;
	// "py" works. The stub must be reported as tried, the absent one not.
	probe := func(cmd string) (bool, bool) {
		switch cmd {
		case "python3":
			return true, false
		case "py":
			return true, true
		}
		return false, false
	}
	python, tried := selectPython([]string{"python", "python3", "py"}, probe)
	if python != "py" {
		t.Errorf("selectPython = %q, want py", python)
	}
	if !reflect.DeepEqual(tried, []string{"python3"}) {
		t.Errorf("tried = %v, want [python3]", tried)
	}

	// Nothing on PATH at all: no interpreter and nothing tried, so no error.
	python, tried = selectPython([]string{"python3", "python"}, func(string) (bool, bool) { return false, false })
	if python != "" || len(tried) != 0 {
		t.Errorf("absent candidates: got %q / %v", python, tried)
	}

	// Candidates exist but none works (Python 2 only): all are reported.
	python, tried = selectPython([]string{"python3", "python"}, func(string) (bool, bool) { return true, false })
	if python != "" || !reflect.DeepEqual(tried, []string{"python3", "python"}) {
		t.Errorf("broken candidates: got %q / %v", python, tried)
	}
}

func TestNoWorkingPythonError(t *testing.T) {
	err := noWorkingPythonError([]string{"python3", "py"})
	if err.Collector != "ai.python" {
		t.Errorf("Collector = %q, want ai.python", err.Collector)
	}
	if !strings.Contains(err.Error, "no working Python 3 interpreter (tried: python3, py)") {
		t.Errorf("Error = %q", err.Error)
	}
	if err.Fatal {
		t.Errorf("a missing Python must not be fatal for the ai collector")
	}
	var _ types.CollectorError = err
}
