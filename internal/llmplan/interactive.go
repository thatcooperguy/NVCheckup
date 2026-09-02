package llmplan

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// IsTerminal reports whether r is an interactive terminal (character device).
func IsTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func readLine(r *bufio.Reader) string {
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

// ParseTokens accepts "32768", "32K", "32k" or "128k".
func ParseTokens(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	mult := 1
	if strings.HasSuffix(s, "k") {
		mult = 1024
		s = strings.TrimSuffix(s, "k")
	}
	n, err := strconv.Atoi(strings.ReplaceAll(s, ",", ""))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n * mult, true
}

// Prompt asks the doctor-style questions of spec 7.1 (model, quantization,
// context, concurrency, runtime, target) and fills o. Blank answers keep the
// defaults, so piped or exhausted input degrades to the defaults.
func Prompt(r *bufio.Reader, w io.Writer, o *Options) error {
	fmt.Fprintln(w, "NVCheckup llm-plan: a few questions, then a read-only sizing plan.")
	fmt.Fprintln(w, "Nothing is downloaded, started or changed on this system.")
	fmt.Fprintln(w)

	// 1. Model
	shapes := Catalogue()
	fmt.Fprintln(w, "1. Which model?")
	for i, m := range shapes {
		fmt.Fprintf(w, "   %d) %s\n", i+1, m.Name)
	}
	fmt.Fprintln(w, "   c) custom shape (parameters, layers, KV heads, head dim)")
	fmt.Fprint(w, "   > ")
	ans := readLine(r)
	switch {
	case strings.EqualFold(ans, "c"):
		if err := promptCustom(r, w, o); err != nil {
			return err
		}
	default:
		idx, err := strconv.Atoi(ans)
		if err == nil && idx >= 1 && idx <= len(shapes) {
			o.Model = shapes[idx-1].ID
		} else if ans != "" {
			if _, ok := FindModel(ans); !ok {
				return fmt.Errorf("unknown model %q", ans)
			}
			o.Model = ans
		} else {
			o.Model = shapes[0].ID
		}
	}
	m, err := ResolveModel(*o)
	if err != nil {
		return err
	}

	// 2. Quantization
	fmt.Fprintln(w)
	fmt.Fprintf(w, "2. Weight format? (%s; default %s)\n", QuantNames(), m.DefaultQuant)
	fmt.Fprint(w, "   > ")
	if ans = readLine(r); ans != "" {
		if _, ok := ParseQuant(ans); !ok {
			return fmt.Errorf("unknown quant %q", ans)
		}
		o.Quant = ans
	}

	// 3. Profile -> defaults for context and concurrency. A blank answer keeps
	// the profile already set (--profile), else chat.
	fmt.Fprintln(w)
	if o.Profile == "" {
		o.Profile = "chat"
	}
	fmt.Fprintf(w, "3. Workload profile? (default %s)\n", o.Profile)
	fmt.Fprintln(w, "   a) chat  (8K context, 1 stream)")
	fmt.Fprintln(w, "   b) agent (32K context, 4 streams = 1 + 3 subagents)")
	fmt.Fprintln(w, "   c) batch (4K context, 8 streams)")
	fmt.Fprintln(w, "   d) rag   (32K context, 1 stream)")
	fmt.Fprint(w, "   > ")
	switch ans = strings.ToLower(readLine(r)); ans {
	case "":
	case "a", "chat":
		o.Profile = "chat"
	case "b", "agent":
		o.Profile = "agent"
	case "c", "batch":
		o.Profile = "batch"
	case "d", "rag":
		o.Profile = "rag"
	default:
		return fmt.Errorf("unknown profile %q", ans)
	}
	def, ok := profileDefaults[o.Profile]
	if !ok {
		return fmt.Errorf("unknown profile %q", o.Profile)
	}

	// 4. Context
	fmt.Fprintln(w)
	fmt.Fprintf(w, "4. Context length per stream in tokens? (e.g. 32K or 131072; default %d)\n", def.ctx)
	fmt.Fprint(w, "   > ")
	if ans = readLine(r); ans != "" {
		n, ok := ParseTokens(ans)
		if !ok {
			return fmt.Errorf("invalid context %q", ans)
		}
		o.Context = n
	}

	// 5. Concurrency
	fmt.Fprintln(w)
	fmt.Fprintf(w, "5. Concurrent streams? (agent: 1 + subagents; default %d)\n", def.conc)
	fmt.Fprint(w, "   > ")
	if ans = readLine(r); ans != "" {
		n, err := strconv.Atoi(ans)
		if err != nil || n <= 0 {
			return fmt.Errorf("invalid concurrency %q", ans)
		}
		o.Concurrency = n
	}

	// 6. Runtime
	fmt.Fprintln(w)
	fmt.Fprintln(w, "6. Runtime? (vllm, trtllm, sglang, llamacpp, ollama; default auto)")
	fmt.Fprint(w, "   > ")
	if ans = readLine(r); ans != "" {
		if _, ok := ParseRuntime(ans); !ok {
			return fmt.Errorf("unknown runtime %q", ans)
		}
		o.Runtime = ans
	}

	// 7. Target. A blank answer keeps the node count already set (--nodes).
	fmt.Fprintln(w)
	fmt.Fprintln(w, "7. Target?")
	fmt.Fprintf(w, "   a) this single node%s\n", defaultMark(o.Nodes != 2))
	fmt.Fprintf(w, "   b) a cluster of two Sparks over ConnectX-7%s\n", defaultMark(o.Nodes == 2))
	fmt.Fprint(w, "   > ")
	switch ans = strings.ToLower(readLine(r)); ans {
	case "":
	case "a", "1", "single", "node":
		o.Nodes = 1
	case "b", "2", "cluster":
		o.Nodes = 2
	default:
		return fmt.Errorf("unknown target %q", ans)
	}
	fmt.Fprintln(w)
	return nil
}

// defaultMark labels the option a blank answer keeps.
func defaultMark(isDefault bool) string {
	if isDefault {
		return " (default)"
	}
	return ""
}

func promptCustom(r *bufio.Reader, w io.Writer, o *Options) error {
	ask := func(label string) (float64, error) {
		fmt.Fprintf(w, "   %s > ", label)
		s := readLine(r)
		if s == "" {
			return 0, nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid %s %q", label, s)
		}
		return v, nil
	}
	var err error
	if o.ParamsB, err = ask("total parameters in billions"); err != nil {
		return err
	}
	if o.ActiveParamsB, err = ask("active parameters in billions (blank = all)"); err != nil {
		return err
	}
	v, err := ask("layers (num_hidden_layers)")
	if err != nil {
		return err
	}
	o.Layers = int(v)
	if v, err = ask("KV heads (num_key_value_heads)"); err != nil {
		return err
	}
	o.KVHeads = int(v)
	if v, err = ask("head dim (head_dim, or hidden_size/num_attention_heads)"); err != nil {
		return err
	}
	o.HeadDim = int(v)
	return nil
}

// RunWithReport builds and prints a plan for an already collected report
// after prompting for the inputs. It is the doctor hand-off (spec 7.1: doctor
// gains one hand-off question) and returns the plan's exit code, or
// types.ExitError on a tool error.
func RunWithReport(r *bufio.Reader, w io.Writer, report *types.Report, goos string) int {
	o := DefaultOptions()
	o.GOOS = goos
	if err := Prompt(r, w, &o); err != nil {
		fmt.Fprintf(w, "Error: %v\n", err)
		return types.ExitError
	}
	pool, notes := DerivePool(report, goos, o.Timeout, o.MemoryGiB, o.Offline)
	ports, known := ListeningPorts(report, goos)
	p, err := Build(report, pool, ports, known, o)
	if err != nil {
		fmt.Fprintf(w, "Error: %v\n", err)
		return types.ExitError
	}
	p.Notes = append(p.Notes, notes...)
	fmt.Fprint(w, RenderText(p))
	return p.ExitCode
}
