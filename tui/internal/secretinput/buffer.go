// Package secretinput provides a bounded overwrite-capable secret editor buffer.
// Secret material is retained only as []byte; View/String never expose raw bytes.
package secretinput

import (
	"errors"
	"unicode/utf8"
)

// MaxBytes is the Core provider-secret-stage body limit (64 KiB).
const MaxBytes = 64 * 1024

// ErrTooLarge is returned when an edit would exceed MaxBytes.
var ErrTooLarge = errors.New("secretinput: exceeds 64 KiB limit")

// Buffer is a bounded byte/rune secret editor. It never stores secrets in
// Go string fields. Call Zero after staging, cancel, or discard.
type Buffer struct {
	data      []byte
	cursor    int // byte offset at a rune boundary
	selActive bool
	selStart  int
	selEnd    int
	overwrite bool
	staged    bool
}

// New returns an empty secret buffer.
func New() *Buffer {
	return &Buffer{}
}

// SetOverwrite enables or disables overwrite typing mode.
func (b *Buffer) SetOverwrite(on bool) {
	if b == nil {
		return
	}
	b.overwrite = on
}

// MoveToStart moves the cursor to the start and clears selection.
func (b *Buffer) MoveToStart() {
	if b == nil {
		return
	}
	b.cursor = 0
	b.clearSelection()
}

// MoveToEnd moves the cursor to the end and clears selection.
func (b *Buffer) MoveToEnd() {
	if b == nil {
		return
	}
	b.cursor = len(b.data)
	b.clearSelection()
}

// SelectAll selects the entire buffer contents.
func (b *Buffer) SelectAll() {
	if b == nil {
		return
	}
	b.selActive = len(b.data) > 0
	b.selStart = 0
	b.selEnd = len(b.data)
	b.cursor = b.selEnd
}

// InsertRune inserts or overwrites a rune at the cursor.
// When a selection is active, the selection is replaced.
func (b *Buffer) InsertRune(r rune) error {
	if b == nil {
		return nil
	}
	var tmp [utf8.UTFMax]byte
	n := utf8.EncodeRune(tmp[:], r)
	return b.ingest(tmp[:n], true)
}

// PasteBytes ingests transient paste octets into the buffer.
// The caller must not retain the source as an application string secret field;
// Buffer itself stores only []byte.
func (b *Buffer) PasteBytes(p []byte) error {
	if b == nil {
		return nil
	}
	return b.ingest(p, false)
}

// Backspace deletes the selection, or the rune before the cursor.
// Deleted bytes are overwritten with zeros before the slice shrinks.
func (b *Buffer) Backspace() {
	if b == nil || len(b.data) == 0 {
		return
	}
	if b.selActive && b.selEnd > b.selStart {
		b.deleteRange(b.selStart, b.selEnd)
		return
	}
	if b.cursor <= 0 {
		return
	}
	_, size := utf8.DecodeLastRune(b.data[:b.cursor])
	if size <= 0 {
		size = 1
	}
	b.deleteRange(b.cursor-size, b.cursor)
}

// DeleteRune deletes the selection, or the rune at the cursor.
// Deleted bytes are overwritten with zeros before the slice shrinks.
func (b *Buffer) DeleteRune() {
	if b == nil || len(b.data) == 0 {
		return
	}
	if b.selActive && b.selEnd > b.selStart {
		b.deleteRange(b.selStart, b.selEnd)
		return
	}
	if b.cursor >= len(b.data) {
		return
	}
	_, size := utf8.DecodeRune(b.data[b.cursor:])
	if size <= 0 {
		size = 1
	}
	b.deleteRange(b.cursor, b.cursor+size)
}

func (b *Buffer) deleteRange(start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(b.data) {
		end = len(b.data)
	}
	if end <= start {
		return
	}
	for i := start; i < end; i++ {
		b.data[i] = 0
	}
	copy(b.data[start:], b.data[end:])
	tail := b.data[len(b.data)-(end-start):]
	for i := range tail {
		tail[i] = 0
	}
	b.data = b.data[:len(b.data)-(end-start)]
	b.cursor = start
	b.clearSelection()
	b.staged = false
}

// PasteRunes ingests transient paste runes without retaining a string field.
func (b *Buffer) PasteRunes(rs []rune) error {
	if b == nil {
		return nil
	}
	if len(rs) == 0 {
		return nil
	}
	size := 0
	for _, r := range rs {
		size += utf8.RuneLen(r)
	}
	buf := make([]byte, 0, size)
	for _, r := range rs {
		buf = utf8.AppendRune(buf, r)
	}
	err := b.ingest(buf, false)
	for i := range buf {
		buf[i] = 0
	}
	return err
}

// Zero overwrites backing memory and clears buffer state.
func (b *Buffer) Zero() {
	if b == nil {
		return
	}
	for i := range b.data {
		b.data[i] = 0
	}
	// Zero unused capacity as well when present.
	if c := cap(b.data); c > 0 {
		full := b.data[:c]
		for i := range full {
			full[i] = 0
		}
		b.data = full[:0]
	} else {
		b.data = nil
	}
	b.cursor = 0
	b.clearSelection()
	b.staged = false
}

// Len returns the current byte length.
func (b *Buffer) Len() int {
	if b == nil {
		return 0
	}
	return len(b.data)
}

// Cap returns the backing array capacity (for zeroing probes).
func (b *Buffer) Cap() int {
	if b == nil {
		return 0
	}
	return cap(b.data)
}

// ByteAt returns backing byte i (0 <= i < Cap). Out of range returns 0.
// Intended for zeroing tests; does not allocate strings.
func (b *Buffer) ByteAt(i int) byte {
	if b == nil || i < 0 || i >= cap(b.data) {
		return 0
	}
	return b.data[:cap(b.data)][i]
}

// HasContent reports whether the buffer holds any bytes.
func (b *Buffer) HasContent() bool {
	return b != nil && len(b.data) > 0
}

// HasUnstaged reports whether the buffer holds bytes not yet marked staged.
func (b *Buffer) HasUnstaged() bool {
	return b != nil && len(b.data) > 0 && !b.staged
}

// MarkStaged marks current contents as staged. HasUnstaged becomes false until
// the buffer is edited again or Zeroed.
func (b *Buffer) MarkStaged() {
	if b == nil {
		return
	}
	b.staged = true
}

// View returns a masked display for UI. It never includes raw secret bytes.
func (b *Buffer) View() string {
	if b == nil || len(b.data) == 0 {
		return ""
	}
	n := utf8.RuneCount(b.data)
	if n <= 0 {
		n = 1
	}
	// Fixed mask character; length conveys size without exposing content.
	buf := make([]byte, 0, n*3)
	for i := 0; i < n; i++ {
		buf = append(buf, 0xe2, 0x80, 0xa2) // UTF-8 '•'
	}
	return string(buf)
}

// String implements fmt.Stringer with the same masking as View.
func (b *Buffer) String() string {
	return b.View()
}

// CopyBytes copies secret bytes into dst. Returns the number of bytes copied.
func (b *Buffer) CopyBytes(dst []byte) int {
	if b == nil || len(b.data) == 0 || len(dst) == 0 {
		return 0
	}
	return copy(dst, b.data)
}

func (b *Buffer) ingest(p []byte, typing bool) error {
	if len(p) == 0 {
		return nil
	}

	start, end := b.selectionOrCursor()
	replaceLen := end - start

	if typing && !b.selActive && b.overwrite && b.cursor < len(b.data) {
		// Overwrite the rune at the cursor.
		_, size := utf8.DecodeRune(b.data[b.cursor:])
		if size <= 0 {
			size = 1
		}
		start = b.cursor
		end = b.cursor + size
		replaceLen = size
	}

	newLen := len(b.data) - replaceLen + len(p)
	if newLen > MaxBytes {
		return ErrTooLarge
	}

	// Ensure capacity and apply replacement without retaining a string.
	if cap(b.data) < newLen {
		next := make([]byte, len(b.data), newLen)
		copy(next, b.data)
		for i := range b.data {
			b.data[i] = 0
		}
		b.data = next
	}

	// Grow/shrink slice for the replacement window.
	delta := len(p) - replaceLen
	if delta > 0 {
		b.data = b.data[:len(b.data)+delta]
		copy(b.data[end+delta:], b.data[end:])
	} else if delta < 0 {
		copy(b.data[start+len(p):], b.data[end:])
		// Zero the tail that will be trimmed.
		tail := b.data[len(b.data)+delta:]
		for i := range tail {
			tail[i] = 0
		}
		b.data = b.data[:len(b.data)+delta]
	}
	copy(b.data[start:start+len(p)], p)
	b.cursor = start + len(p)
	b.clearSelection()
	b.staged = false
	return nil
}

func (b *Buffer) selectionOrCursor() (start, end int) {
	if b.selActive && b.selEnd > b.selStart {
		return b.selStart, b.selEnd
	}
	return b.cursor, b.cursor
}

func (b *Buffer) clearSelection() {
	b.selActive = false
	b.selStart = 0
	b.selEnd = 0
}
