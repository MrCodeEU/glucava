package report

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

//go:embed report.typ
var templateSrc []byte

//go:embed fonts/Inter-Regular.ttf
var fontRegular []byte

//go:embed fonts/Inter-Bold.ttf
var fontBold []byte

// ErrNoTypst means the Typst binary was not found; the web handler turns it
// into a 503 with a hint instead of a 500.
var ErrNoTypst = errors.New("the typst binary is not installed")

// EnvTypst overrides the path of the Typst binary.
const EnvTypst = "GLUCAVA_TYPST"

const (
	compileTimeout = 30 * time.Second
	maxOutputBytes = 20 << 20 // PDF or PNG page cap
	maxStderrBytes = 32 << 10
)

// FindTypst returns the Typst binary: $GLUCAVA_TYPST, else "typst" on PATH.
func FindTypst() (string, error) {
	if p := os.Getenv(EnvTypst); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("%w: %s=%s: %v", ErrNoTypst, EnvTypst, p, err)
		}
		return p, nil
	}
	p, err := exec.LookPath("typst")
	if err != nil {
		return "", ErrNoTypst
	}
	return p, nil
}

// PDF typesets the report and returns the PDF bytes.
func (m *Model) PDF(ctx context.Context, typst string) ([]byte, error) {
	pages, err := m.compile(ctx, typst, "pdf", 0)
	if err != nil {
		return nil, err
	}
	return pages[0], nil
}

// PNGs typesets the report as one PNG per page at ppi. It exists for the
// visual check of the layout (see report_test.go), not for the web route.
func (m *Model) PNGs(ctx context.Context, typst string, ppi int) ([][]byte, error) {
	return m.compile(ctx, typst, "png", ppi)
}

// compile writes the template, data, charts and the embedded font into a
// private temp directory, runs Typst there and reads the result back. The
// directory is the Typst root, so the template cannot read anything else, and
// only the embedded font is used, so the output is the same on every host.
func (m *Model) compile(ctx context.Context, typst, format string, ppi int) ([][]byte, error) {
	files, err := m.Files()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "glucava-report-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := os.Mkdir(filepath.Join(dir, "fonts"), 0o700); err != nil {
		return nil, err
	}
	write := func(name string, b []byte) error { return os.WriteFile(filepath.Join(dir, name), b, 0o600) }
	for name, b := range files {
		if err := write(name, b); err != nil {
			return nil, err
		}
	}
	for name, b := range map[string][]byte{"report.typ": templateSrc, "fonts/Inter-Regular.ttf": fontRegular, "fonts/Inter-Bold.ttf": fontBold} {
		if err := write(name, b); err != nil {
			return nil, err
		}
	}

	out := "out.pdf"
	if format == "png" {
		out = "out-{p}.png"
	}
	args := []string{"compile", "--root", dir, "--font-path", filepath.Join(dir, "fonts"), "--ignore-system-fonts",
		"--creation-timestamp", strconv.FormatInt(m.In.Now.Unix(), 10), "--format", format}
	if format == "png" && ppi > 0 {
		args = append(args, "--ppi", strconv.Itoa(ppi))
	}
	args = append(args, filepath.Join(dir, "report.typ"), filepath.Join(dir, out))

	ctx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, typst, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second                                    // a killed typst must not be held up by a stuck pipe
	cmd.Env = []string{"HOME=" + dir, "TMPDIR=" + dir, "LANG=C.UTF-8"} // no proxy, no font or package paths from the host
	var stderr limitedBuffer
	cmd.Stderr, cmd.Stdout = &stderr, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("typst timed out after %s", compileTimeout)
		}
		return nil, fmt.Errorf("typst: %w: %s", err, stderr.String())
	}

	if format == "pdf" {
		b, err := readCapped(filepath.Join(dir, "out.pdf"))
		if err != nil {
			return nil, err
		}
		return [][]byte{b}, nil
	}
	var pages [][]byte
	for i := 1; ; i++ {
		b, err := readCapped(filepath.Join(dir, fmt.Sprintf("out-%d.png", i)))
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		pages = append(pages, b)
	}
	if len(pages) == 0 {
		return nil, errors.New("typst produced no pages")
	}
	return pages, nil
}

func readCapped(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxOutputBytes {
		return nil, fmt.Errorf("typst output is %d bytes, over the %d byte cap", st.Size(), maxOutputBytes)
	}
	return os.ReadFile(path)
}

// limitedBuffer keeps the first maxStderrBytes of what Typst prints, so a
// runaway diagnostic cannot fill memory.
type limitedBuffer struct{ buf bytes.Buffer }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxStderrBytes - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string { return l.buf.String() }
