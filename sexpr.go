package sexpr

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxToken = 1024 * 1024 * 1024 // should be more than enough
	eof      = -1
	rivest   = false // enforce rule that token cannot start with digit
)

// Form selects between Canonical or Advanced format.
type Form int

const (
	Canonical Form = iota // format with sized byte strings
	Advanced              // format with whitespace, quoted strings, etc
)

// Expr represents an s-expression: sexpr ::= string | (sexpr*).
// The `string' is any byte string, but this package distinguishes
// printable text or tokens from binary data, partly for Go application
// convenience, partly to guide the output representation.
// For a similar reason, the IsList, Op and Args methods are
// included in the interface, even though they are not essential.
// Expr implements both binary and text encodings
// (BinaryAppender and TextAppender), producing the
// Canonical and Advanced formats respectiively.
type Expr interface {
	// IsList tells whether the Expr is an inner node, a list.
	IsList() bool

	// Equal tells whether e1 is equal to e2 by ``deep comparison''.
	// String and Binary compare the underlying byte strings.
	Equal(Expr) bool

	// Copy returns a copy of an expression (``deep copy'').
	Copy() Expr

	// Els returns the elements of an expression: the list, or a single element if it is a leaf.
	Els() []Expr

	// Op returns the text of the `operator' of an expression.
	// The operator is the string itself in the case of a String,
	// or the initial expression in a list, if that is a text expression.
	// Otherwise the result is the empty string.
	Op() string

	// Args returns the arguments to an operator, or nil if there are none.
	Args() []Expr

	fmt.Stringer
	encoding.BinaryAppender
	encoding.TextAppender
}

// leaf identifies a leaf node.
type leaf struct{}

func (l *leaf) IsList() bool {
	return false
}

func (l *leaf) Args() []Expr {
	return nil
}

// String is a leaf node that has text.
type String struct {
	leaf
	S    string
	Hint string
}

// NewString returns a new text leaf.
func NewString(s string) *String {
	return &String{S: s}
}

// NewHintedString returns a new text leaf with a presentation hint.
func NewHintedString(s, hint string) *String {
	return &String{S: s, Hint: hint}
}

// Op returns the string as an operator name.
func (s *String) Op() string {
	return s.S
}

// Els returns the string as a singleton List.
func (s *String) Els() []Expr {
	return []Expr{s}
}

// Equal reports whether s has the same value as e.
// String and Binary compare byte strings.
func (s *String) Equal(e Expr) bool {
	switch t := e.(type) {
	case nil:
		return s == nil
	case *String:
		return s.S == t.S && s.Hint == t.Hint
	case *Binary:
		return bytes.Equal([]byte(s.S), t.Data) && s.Hint == t.Hint
	default:
		return false
	}
}

// Copy returns a copy of s.
func (s *String) Copy() Expr {
	return &String{S: s.S, Hint: s.Hint}
}

// AppendText implements the TextAppender interface for a
// token or quoted string, appending the textual form to slice d
// and returning the updated slice without error.
func (s *String) AppendText(d []byte) ([]byte, error) {
	if s.Hint == "" && IsToken(s.S) {
		return append(d, s.S...), nil
	}
	if s.Hint != "" {
		d = append(d, '[')
		d = appendHint(d, s.Hint)
		d = append(d, ']')
	}
	d = append(d, quote(s.S)...)
	return d, nil
}

// AppendBinary implements the BinaryAppender interface:
// the binary version of a string in an S-expression is added to d.
// The updated slice is returned without error.
func (s *String) AppendBinary(d []byte) ([]byte, error) {
	if s.Hint != "" {
		d = packHint(d, s.Hint)
	}
	return packBytes(d, []byte(s.S)), nil
}

func (s *String) String() string {
	d, _ := s.AppendText(nil)
	return string(d)
}

// Binary is a leaf node that has binary data.
type Binary struct {
	leaf
	Data []byte
	Hint string
}

// NewBinary returns a leaf node with a slice of data.
func NewBinary(data []byte) *Binary {
	return &Binary{Data: data}
}

// NewHintedBinary returns a leaf node with a slice of data and
// associated presentation hint.
func NewHintedBinary(data []byte, hint string) *Binary {
	return &Binary{Data: data, Hint: hint}
}

// Els returns b as a singleton list.
func (b *Binary) Els() []Expr {
	return []Expr{b}
}

// Op returns the empty string, since there is no textual operator.
func (b *Binary) Op() string {
	return ""
}

// Equal reports whether b has the same value as e, including hint.
// String and Binary compare byte strings.
func (b *Binary) Equal(e Expr) bool {
	switch t := e.(type) {
	case nil:
		return b == nil
	case *String:
		return bytes.Equal([]byte(t.S), b.Data) && b.Hint == t.Hint
	case *Binary:
		return bytes.Equal(b.Data, t.Data) && b.Hint == t.Hint
	default:
		return false
	}
}

// Copy returns a copy of b as a new Expr.
func (b *Binary) Copy() Expr {
	return &Binary{Data: bytes.Clone(b.Data), Hint: b.Hint}
}

// AppendText implements the TextAppender interface for a
// binary string, appending its textual form to slice d
// and returning the updated slice without error.
func (b *Binary) AppendText(d []byte) ([]byte, error) {
	if b.Hint != "" {
		d = append(d, '[')
		d = appendHint(d, b.Hint)
		d = append(d, ']')
	}
	if len(b.Data) <= 8 {
		d = append(d, '#')
		d = hex.AppendEncode(d, b.Data)
		d = append(d, '#')
		return d, nil
	}
	d = append(d, '|')
	d = base64.StdEncoding.AppendEncode(d, b.Data)
	d = append(d, '|')
	return d, nil
}

// AppendBinary implements the BinaryAppender interface:
// it appends the binary representation of a Binary leaf to
// d and returns the updated slice, without error.
func (b *Binary) AppendBinary(d []byte) ([]byte, error) {
	if b.Hint != "" {
		d = packHint(d, b.Hint)
	}
	return packBytes(d, b.Data), nil
}

func (b *Binary) String() string {
	d, _ := b.AppendText(nil)
	return string(d)
}

// List is an interior node: a list of expressions.
type List []Expr

// NewList returns a new list = (list | string)*
// from the element arguments,
// each of type string, []byte or Expr.
// Other types produce a panic.
func NewList(els ...any) List {
	l := make([]Expr, len(els))
	for i, e := range els {
		switch v := e.(type) {
		case string:
			l[i] = NewString(v)
		case []byte:
			l[i] = NewBinary(v)
		case Expr:
			l[i] = v
		default:
			panic("unexpected type to NewList")
		}
	}
	return l
}

func (l List) isLeaf() bool {
	return false
}

// IsList reports that l is a list.
func (l List) IsList() bool {
	return true
}

// Op returns the operator name, the first String in the list,
// or an empty string if there is no operator.
func (l List) Op() string {
	if len(l) == 0 {
		return ""
	}
	if s, ok := l[0].(*String); ok {
		return s.S
	}
	return ""
}

// Args returns the operands to a list's operator,
// an empty list if there are none.
func (l List) Args() []Expr {
	if len(l) == 0 {
		return []Expr{}
	}
	return l[1:]
}

// Els returns the elements of the expression.
func (l List) Els() []Expr {
	return l
}

func (l List) Equal(e Expr) bool {
	t, ok := e.(List)
	if !ok || len(l) != len(t) {
		return false
	}
	for i, el := range l {
		if !el.Equal(t[i]) {
			return false
		}
	}
	return true
}

func (l List) Copy() Expr {
	if l == nil {
		return nil
	}
	t := make([]Expr, len(l))
	for i, e := range l {
		if e != nil {
			t[i] = e.Copy()
		}
	}
	return List(t)
}

func (l List) stringTo(sb *strings.Builder) {
	if len(l) == 0 {
		sb.WriteString("()")
		return
	}
}

func (l List) String() string {
	if l == nil {
		return "()"
	}
	var sb strings.Builder
	sb.WriteByte('(')
	for i, el := range l {
		if i != 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(el.String())
	}
	sb.WriteByte(')')
	return sb.String()
}

// Head returns the first element of list l, or nil if none.
func (l List) Head() Expr {
	if len(l) == 0 {
		return nil
	}
	return l[0]
}

// Tail returns the second and subsequent elements of list l,
// or nil if none.
func (l List) Tail() List {
	if len(l) == 0 {
		return nil
	}
	return l[1:]
}

// AppendBinary implements the BinaryAppender interface:
// it appends the binary representation of a list to
// d and returns the updated slice without error.
func (l List) AppendBinary(d []byte) ([]byte, error) {
	d = append(d, '(')
	for _, el := range l {
		d, _ = el.AppendBinary(d)
	}
	return append(d, ')'), nil
}

// AppendText implements the TextAppender interface:
// it appends the text (“advanced”) representation of a list to
// d and returns the updated slice without error.
func (l List) AppendText(d []byte) ([]byte, error) {
	d = append(d, '(')
	for i, el := range l {
		if i != 0 {
			d = append(d, ' ')
		}
		d, _ = el.AppendText(d)
	}
	d = append(d, ')')
	return d, nil
}

// SyntaxError describes a syntax error, including its location in the input stream.
type SyntaxError struct {
	Msg    string
	Offset int64
}

func (e SyntaxError) Error() string {
	return fmt.Sprintf("offset %d: %s", e.Offset, e.Msg)
}

// Reader represents a stream of S-expressions.
type Reader struct {
	rd     io.Reader
	buf    []byte
	nb     int
	w      int
	offset int64
	err    error
}

func (rd *Reader) get() rune {
	for rd.nb < utf8.UTFMax && !utf8.FullRune(rd.buf[0:rd.nb]) {
		if rd.err != nil {
			return eof
		}
		n, err := rd.rd.Read(rd.buf[rd.nb : rd.nb+1])
		if err != nil {
			rd.err = err
			if n == 0 {
				return eof
			}
		}
		rd.nb += n
	}
	rd.offset++
	c, w := utf8.DecodeRune(rd.buf[0:rd.nb])
	rd.nb = 0
	rd.w = w
	return c
}

func (rd *Reader) unget() {
	if rd.err != nil {
		return
	}
	rd.nb = rd.w
	rd.offset--
}

// readFull reads exactly n bytes from the input.
// It is used only when the unget buffer is empty.
func (rd *Reader) readFull(buf []byte) error {
	if rd.err != nil {
		return rd.err
	}
	_, err := io.ReadFull(rd.rd, buf)
	rd.err = err
	return err
}

// NewReader returns an S-expression reader for the given stream.
func NewReader(f io.Reader) *Reader {
	return &Reader{rd: f, buf: make([]byte, utf8.UTFMax)}
}

// Read returns the next S-expression from the stream, or nil and an error.
func (rd *Reader) Read() (Expr, error) {
	return rd.parseItem()
}

// Parse parses the given string as an S-expression.,
// It returns the expression or an error.
func Parse(s string) (Expr, error) {
	rd := NewReader(strings.NewReader(s))
	e, err := rd.Read()
	if err != nil {
		return nil, err
	}
	o := rd.offset
	if rd.get() != eof {
		return nil, &SyntaxError{"missing operator or extra expression", o}
	}
	return e, nil
}

// parseitem parses item = { base64expr } | ( item* ) | display? simple-string.
func (rd *Reader) parseItem() (Expr, error) {
	p0 := rd.offset
	c := rd.skipWS()
	if c < 0 {
		return nil, rd.err
	}
	switch c {
	case '{':
		dec, err := rd.readEncoding('}', base64dec)
		if err != nil {
			return nil, err
		}
		// nested reader because the {...} is self-contained
		nrd := NewReader(bytes.NewReader(dec))
		e, err := nrd.parseItem()
		if err != nil {
			return e, addOffset(err, rd.offset)
		}
		return e, nil
	case '(':
		els := []Expr{}
		for {
			c := rd.skipWS()
			if c < 0 {
				return nil, &SyntaxError{"unclosed '('", p0}
			}
			if c == ')' {
				break
			}
			rd.unget()
			exp, err := rd.parseItem() // we'll catch missing ) at top of loop
			if err != nil {
				return nil, err
			}
			els = append(els, exp)
		}
		return List(els), nil
	case '[':
		// display hint
		hint, err := rd.simpleString(rd.get(), "")
		if err != nil {
			return nil, err
		}
		c = rd.skipWS()
		if c != ']' {
			if c >= 0 {
				rd.unget()
			}
			return nil, &SyntaxError{"missing ] in display hint", p0}
		}
		if v, ok := hint.(*String); !ok {
			return nil, &SyntaxError{"illegal display hint", rd.offset}
		} else {
			return rd.simpleString(rd.skipWS(), v.S)
		}
	default:
		return rd.simpleString(c, "")
	}
}

// addOffset adds an outer byte offset to the SyntaxError's
// offset from a separately-parsed inner expression.
// It is used by the {...} syntax.
func addOffset(e error, offset int64) error {
	if synerr, ok := e.(*SyntaxError); ok {
		se := *synerr // leave original untouched
		se.Offset += offset
		return &se
	}
	return e
}

// isSpace reports whether c is a space according to the Rivest spec.
func isSpace(c rune) bool {
	return c == ' ' || c == '\r' || c == '\t' || c == '\n'
}

// skipWS returns the first non-white-space character;
// returning instead eof on an error including EOF.
func (rd *Reader) skipWS() rune {
	for {
		c := rd.get()
		if !isSpace(c) {
			return c
		}
	}
}

// decimal collects an optional decimal prefix [1-9]|[0-9]+ | 0,
// returning the next character to process.
func (rd *Reader) decimal(sb *strings.Builder, c rune) rune {
	if c == '0' {
		sb.WriteRune(c)
		return rd.get()
	}
	if c >= '1' && c <= '9' {
		for ; c >= '0' && c <= '9'; c = rd.get() {
			sb.WriteRune(c)
		}
	}
	return c
}

func (rd *Reader) simpleString(c rune, hint string) (Expr, error) {
	// the "optional length field" gives the length of the resulting
	// byte string, for a base64 or quoted string.
	// here, it is collected but otherwise unused,
	// unless it forms the prefix for a token (if rivest is false).
	var tok strings.Builder
	// optional byte size in decimal for quoted strings and base64
	// if rivest is false, also digits starting a token
	c = rd.decimal(&tok, c)
	switch c {
	case '"':
		text, err := rd.unquote()
		if err != nil {
			return nil, err
		}
		return &String{S: text, Hint: hint}, nil
	case '|':
		data, err := rd.readEncoding(c, base64dec)
		if err != nil {
			return nil, err
		}
		return &Binary{Data: data, Hint: hint}, nil
	case '#':
		if tok.Len() != 0 {
			return nil, &SyntaxError{"illegal length before hex string", rd.offset}
		}
		data, err := rd.readEncoding(c, hex.DecodeString)
		if err != nil {
			return nil, err
		}
		return &Binary{Data: data, Hint: hint}, nil
	default:
		if tok.Len() != 0 {
			if c == ':' { // raw bytes
				nbytes, err := strconv.ParseUint(tok.String(), 10, 64)
				if err != nil {
					return nil, &SyntaxError{err.Error(), rd.offset}
				}
				if nbytes > MaxToken {
					return nil, &SyntaxError{"implausible token length", rd.offset}
				}
				a := make([]byte, nbytes)
				err = rd.readFull(a)
				if err != nil {
					return nil, err
				}
				return sform(a, hint)
			}
			if rivest {
				return nil, &SyntaxError{"token can't start with a digit", rd.offset - int64(tok.Len()) - 1}
			}
		}
		// <token> by definition is always printable; never utf-8
		// utf-8 can appear only in a quoted string.
		for IsTokenRune(c) {
			tok.WriteRune(c)
			c = rd.get()
		}
		if tok.Len() == 0 {
			return nil, &SyntaxError{"missing token", rd.offset} // consume c to ensure progress on error
		}
		if c != eof {
			rd.unget()
		}
		return &String{S: tok.String(), Hint: hint}, nil
	}
}

// readEncoding collects text up to an end character, that contains an encoded
// expression, and returns the decoded text.
func (rd *Reader) readEncoding(end rune, decode func(string) ([]byte, error)) ([]byte, error) {
	s, err := rd.toClosing(end)
	if err != nil {
		return nil, err
	}
	dec, err := decode(s)
	if err != nil {
		return nil, fmt.Errorf("encoded value %.8q...: %w", s, err)
	}
	return dec, nil
}

// sform decides whether a given sequence of bytes is best
// regarded as text or binary.
func sform(a []byte, hint string) (Expr, error) {
	if isText(a) {
		return &String{S: string(a), Hint: hint}, nil
	}
	return &Binary{Data: a, Hint: hint}, nil
}

// toClosing reads until the end character, and returns
// the result as a string, skipping enclosed white space.
func (rd *Reader) toClosing(end rune) (string, error) {
	var sb strings.Builder
	p0 := rd.offset
	for {
		switch c := rd.get(); {
		case c == end:
			return sb.String(), nil
		case c < 0:
			return "", &SyntaxError{fmt.Sprintf("missing closing '%c'", end), p0}
		case isSpace(c):
			// ignored
		default:
			sb.WriteRune(c)
		}
	}
}

func hexDigit(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c) - '0'
	case c >= 'a' && c <= 'f':
		return 10 + (int(c) - 'a')
	case c >= 'A' && c <= 'F':
		return 10 + (int(c) - 'A')
	default:
		return -1
	}
}

// unquote strips the quotes from the next string in the input and returns it.
// Escape sequences are converted to the underlying byte.
func (rd *Reader) unquote() (string, error) {
	var sb strings.Builder
	p0 := rd.offset
	for {
		c := rd.get()
		if c < 0 {
			return sb.String(), &SyntaxError{"unclosed quoted string", p0}
		}
		if c != '\\' {
			if c == '"' {
				break
			}
			sb.WriteRune(c)
			continue
		}
		e0 := rd.offset
		c = rd.get()
		if c < 0 {
			break
		}
		switch c {
		case '\r':
			c = rd.get()
			if c != '\n' {
				rd.unget()
			}
			continue
		case '\n':
			c = rd.get()
			if c != '\r' {
				rd.unget()
			}
			continue
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'v':
			sb.WriteByte('\v')
		case '0', '1', '2', '3', '4',
			'5', '6', '7', '8', '9':
			rd.unget()
			oct := 0
			for i := 0; i < 3; i++ {
				c = rd.get()
				if !(c >= '0' && c <= '7') {
					return sb.String(), &SyntaxError{"illegal octal escape", e0}
				}
				oct = (oct << 3) | (int(c) - '0')
			}
			sb.WriteByte(byte(oct))
		case 'x':
			c0 := hexDigit(rd.get())
			c1 := hexDigit(rd.get())
			if c0 < 0 || c1 < 0 {
				return "", &SyntaxError{"illegal hex escape", e0}
			}
			sb.WriteByte(byte((c0 << 4) | c1))
		default:
			sb.WriteRune(c) // as-is, allows for utf-8
		}
	}
	return sb.String(), nil
}

// packBytes appends data to a, returning the updated slice.
func packBytes(a []byte, data []byte) []byte {
	n := len(data)
	a = fmt.Appendf(a, "%d:", n)
	a = append(a, data...)
	return a
}

// packHint appends a `hint' to a, returning the updated slice.
// Nothing is added if the hint is empty.
func packHint(a []byte, hint string) []byte {
	if hint == "" {
		return a
	}
	a = append(a, '[')
	a = packBytes(a, []byte(hint))
	a = append(a, ']')
	return a
}

func base64dec(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func Base64(e Expr, form Form) string {
	var a []byte
	if form == Advanced {
		a, _ = e.AppendText(nil)
	} else {
		a, _ = e.AppendBinary(nil)
	}
	o := []byte{'{'}
	o = base64.StdEncoding.AppendEncode(o, a)
	o = append(o, '}')
	return string(o)
}

// IsTokenRune returns true iff rune r can appear in a token.
func IsTokenRune(r rune) bool {
	return r >= '0' && r <= '9' ||
		r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r == '-' || r == '.' || r == '/' || r == '_' || r == ':' || r == '*' || r == '+' || r == '='
}

// IsToken enforces the following rule:
//
// An octet string that meets the following conditions may be given
// directly as a "token".
//
//	-- it does not begin with a digit
//
//	-- it contains only characters that are
//		-- alphabetic (upper or lower case),
//		-- numeric, or
//		-- one of the eight "pseudo-alphabetic" punctuation marks:
//			-   .   /   _   :  *  +  =
//	(Note: upper and lower case are not equivalent.)
//	(Note: A token may begin with punctuation, including ":").
func IsToken(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 && c >= '0' && c <= '9' {
			// "it does not start with a digit"
			return false
		}
		if !IsTokenRune(c) {
			return false
		}
	}
	return true
}

// isText reports whether the data should qualify as binary or text.
// The distinction is only for the interface to Go and
// other languages where strings and binary are slightly different.
func isText(a []byte) bool {
	for i := 0; i < len(a); {
		r, w := utf8.DecodeRune(a[i:])
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return false
		}
		i += w
	}
	return true
}

// quote returns s quoted if necessary, following the quoting rules of the spec.
func quote(s string) string {
	if IsToken(s) {
		// no quoting required
		return s
	}
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			sb.WriteString("\\\"")
		case '\\':
			sb.WriteString("\\\\")
		case '\b':
			sb.WriteString("\\b")
		case '\f':
			sb.WriteString("\\f")
		case '\n':
			sb.WriteString("\\n")
		case '\t':
			sb.WriteString("\\t")
		case '\r':
			sb.WriteString("\\r")
		case '\v':
			sb.WriteString("\\v")
		default:
			sb.WriteByte(s[i])
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func appendHint(d []byte, hint string) []byte {
	return append(d, quote(hint)...)
}

// AsData returns the value of a leaf expression as
// data bytes. A non-leaf expression has none,
// represented as nil.
func AsData(e Expr) []byte {
	switch r := e.(type) {
	case *String:
		return []byte(r.S)
	case *Binary:
		return r.Data
	default:
		return nil
	}
}

// AsText returns the value of a leaf expression as
// textual data. A non-leaf expression has none,
// represented as the empty string "".
func AsText(e Expr) string {
	switch r := e.(type) {
	case *String:
		return r.S
	case *Binary:
		return string(r.Data)
	default:
		return ""
	}
}
