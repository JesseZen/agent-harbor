package secretinput_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/asheshgoplani/agent-deck/internal/secretinput"
)

// testProbe is a non-production sentinel used only for buffer assertions.
// It is never a real credential.
var testProbe = []byte{0xAA, 0xAA, 0xAA, 0xAA, 0xBB, 0xBB, 0xBB, 0xBB}

func TestOverwriteModeReplacesRuneAtCursor(t *testing.T) {
	b := secretinput.New()
	if err := b.PasteBytes([]byte{'A', 'B', 'C'}); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	b.MoveToStart()
	b.SetOverwrite(true)
	if err := b.InsertRune('X'); err != nil {
		t.Fatalf("InsertRune: %v", err)
	}

	got := copyOut(t, b)
	want := []byte{'X', 'B', 'C'}
	if !bytes.Equal(got, want) {
		t.Fatalf("overwrite content = %v, want %v", got, want)
	}
}

func TestTypingReplacesSelection(t *testing.T) {
	b := secretinput.New()
	if err := b.PasteBytes([]byte{'A', 'B', 'C', 'D'}); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	b.SelectAll()
	if err := b.InsertRune('Z'); err != nil {
		t.Fatalf("InsertRune: %v", err)
	}

	got := copyOut(t, b)
	want := []byte{'Z'}
	if !bytes.Equal(got, want) {
		t.Fatalf("selection replace = %v, want %v", got, want)
	}
}

func TestPasteBytesDoesNotRetainStringField(t *testing.T) {
	b := secretinput.New()
	if err := b.PasteBytes(testProbe); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}

	v := reflect.ValueOf(b).Elem()
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		// Unexported string fields are inaccessible via Interface; use String()
		// only when CanInterface, otherwise skip (Buffer must not need them).
		if !field.CanInterface() {
			t.Fatalf("unexported string field %q present; secret buffers must not retain string secret fields", typ.Field(i).Name)
		}
		if field.String() != "" && bytes.Contains([]byte(field.String()), testProbe) {
			t.Fatalf("string field %q retains probe bytes", typ.Field(i).Name)
		}
	}
	if !b.HasContent() || b.Len() != len(testProbe) {
		t.Fatalf("HasContent/Len after paste: content=%v len=%d", b.HasContent(), b.Len())
	}
}

func TestPasteRunesIngestWithoutStringRetention(t *testing.T) {
	b := secretinput.New()
	runes := []rune{'α', 'β', 'γ'}
	if err := b.PasteRunes(runes); err != nil {
		t.Fatalf("PasteRunes: %v", err)
	}
	wantLen := 0
	for _, r := range runes {
		wantLen += utf8.RuneLen(r)
	}
	if b.Len() != wantLen {
		t.Fatalf("Len = %d, want %d", b.Len(), wantLen)
	}
	got := copyOut(t, b)
	want := make([]byte, 0, wantLen)
	for _, r := range runes {
		want = utf8.AppendRune(want, r)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("PasteRunes content mismatch: got %v want %v", got, want)
	}
	// Ensure no exported/unexported string field holds the UTF-8 form as a stable field.
	v := reflect.ValueOf(b).Elem()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Kind() == reflect.String {
			t.Fatalf("Buffer must not declare string fields (found %q)", v.Type().Field(i).Name)
		}
	}
}

func TestZeroClearsLengthAndBackingMemory(t *testing.T) {
	b := secretinput.New()
	payload := bytes.Repeat([]byte{0xAA}, 64)
	if err := b.PasteBytes(payload); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	capBefore := b.Cap()
	if capBefore < len(payload) {
		t.Fatalf("Cap = %d, want >= %d", capBefore, len(payload))
	}

	b.Zero()

	if b.Len() != 0 || b.HasContent() || b.HasUnstaged() {
		t.Fatalf("after Zero: Len=%d HasContent=%v HasUnstaged=%v", b.Len(), b.HasContent(), b.HasUnstaged())
	}
	for i := 0; i < capBefore; i++ {
		if got := b.ByteAt(i); got != 0 {
			t.Fatalf("backing[%d] = 0x%02X, want 0x00 after Zero", i, got)
		}
	}
	out := make([]byte, 8)
	if n := b.CopyBytes(out); n != 0 {
		t.Fatalf("CopyBytes after Zero wrote %d bytes", n)
	}
}

func TestRejectsOverflowWithoutRetainingOverflowBytes(t *testing.T) {
	b := secretinput.New()
	full := bytes.Repeat([]byte{0xAA}, secretinput.MaxBytes)
	if err := b.PasteBytes(full); err != nil {
		t.Fatalf("PasteBytes full: %v", err)
	}
	err := b.PasteBytes([]byte{0xCC})
	if !errors.Is(err, secretinput.ErrTooLarge) {
		t.Fatalf("overflow err = %v, want ErrTooLarge", err)
	}
	if b.Len() != secretinput.MaxBytes {
		t.Fatalf("Len after reject = %d, want %d", b.Len(), secretinput.MaxBytes)
	}
	// Overflow byte must not appear as a trailing addition.
	last := b.ByteAt(b.Len() - 1)
	if last == 0xCC {
		t.Fatalf("overflow byte 0xCC retained in buffer")
	}
	// Error text must not include payload.
	if strings.Contains(err.Error(), "\xcc") || strings.Contains(err.Error(), string([]byte{0xCC})) {
		t.Fatalf("error retained overflow payload: %q", err.Error())
	}
}

func TestInsertRuneRejectsWhenAtLimit(t *testing.T) {
	b := secretinput.New()
	if err := b.PasteBytes(bytes.Repeat([]byte{'A'}, secretinput.MaxBytes)); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	b.SetOverwrite(false)
	if err := b.InsertRune('B'); !errors.Is(err, secretinput.ErrTooLarge) {
		t.Fatalf("InsertRune at limit err = %v, want ErrTooLarge", err)
	}
	if b.Len() != secretinput.MaxBytes {
		t.Fatalf("Len = %d, want %d", b.Len(), secretinput.MaxBytes)
	}
}

func TestViewAndStringNeverExposeRawBytes(t *testing.T) {
	b := secretinput.New()
	probe := []byte{'Z', 'Z', 'U', 'N', 'I', 'Q', 'U', 'E', 'P', 'R', 'O', 'B', 'E', 'Z', 'Z'}
	if err := b.PasteBytes(probe); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	view := b.View()
	str := b.String()
	if strings.Contains(view, string(probe)) {
		t.Fatalf("View leaked probe: %q", view)
	}
	if strings.Contains(str, string(probe)) {
		t.Fatalf("String leaked probe: %q", str)
	}
	for _, p := range probe {
		if strings.ContainsRune(view, rune(p)) && p >= 'A' && p <= 'Z' {
			// Mask may use bullets only; any ASCII letter from probe is a leak.
			t.Fatalf("View contains probe rune %q: %q", p, view)
		}
	}
	if view == "" && b.HasContent() {
		t.Fatal("View empty while buffer has content")
	}
}

func TestHasUnstagedTracksStagingLifecycle(t *testing.T) {
	b := secretinput.New()
	if b.HasUnstaged() {
		t.Fatal("empty buffer should not be unstaged")
	}
	if err := b.PasteBytes(testProbe); err != nil {
		t.Fatalf("PasteBytes: %v", err)
	}
	if !b.HasUnstaged() {
		t.Fatal("HasUnstaged want true after paste")
	}
	b.MarkStaged()
	if b.HasUnstaged() {
		t.Fatal("HasUnstaged want false after MarkStaged")
	}
	// Editing again marks unstaged.
	if err := b.InsertRune('!'); err != nil {
		t.Fatalf("InsertRune: %v", err)
	}
	if !b.HasUnstaged() {
		t.Fatal("HasUnstaged want true after edit")
	}
	b.Zero()
	if b.HasUnstaged() {
		t.Fatal("HasUnstaged want false after Zero")
	}
}

func TestPackageProductionCodeNeverFormatsBufferPayload(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			fn := sel.Sel.Name
			if pkg.Name != "fmt" && pkg.Name != "log" {
				return true
			}
			switch fn {
			case "Errorf", "Sprintf", "Printf", "Fprintf", "Appendf":
				// Disallow format verbs that could embed secret bytes.
				if len(call.Args) == 0 {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("%s: %s.%s must use a string literal format", name, pkg.Name, fn)
					return true
				}
				format := lit.Value
				for _, verb := range []string{`%s`, `%q`, `%x`, `%X`, `%v`, `%#v`} {
					if strings.Contains(format, verb) {
						t.Errorf("%s: %s.%s format %s may embed buffer payload; use sentinel errors / redaction helpers", name, pkg.Name, fn, format)
					}
				}
			}
			return true
		})
	}
}

func TestRedactedErrorOmitsPayload(t *testing.T) {
	err := secretinput.RedactError(errors.New(string(testProbe)))
	if err == nil {
		t.Fatal("RedactError returned nil")
	}
	if strings.Contains(err.Error(), string(testProbe)) {
		t.Fatalf("RedactError retained probe: %q", err.Error())
	}
}

func copyOut(t *testing.T, b *secretinput.Buffer) []byte {
	t.Helper()
	dst := make([]byte, b.Len())
	n := b.CopyBytes(dst)
	if n != b.Len() {
		t.Fatalf("CopyBytes wrote %d, len=%d", n, b.Len())
	}
	return dst
}
