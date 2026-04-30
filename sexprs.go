// Package sexprs provides a full SDSI/SPKI S-expression reader.
package sexprs

import (
	"bytes"
	"encoding"
	"encoding/hex"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const MaxToken = 1024*1024  // should be more than enough

const eof = -1

type Expr interface {
	isLeaf() bool
	IsList() bool

	// Els returns the list of subexpressions of an expression,
	// or nil if the expression is nil or not a list.
	Els() []Expr

	// Op returns the text of the operator of the expression.
	// The operator is the string itself in the case of a text expression,
	// or the initial expression in a list, if that is a text expression.
	Op() string

	// Args returns the arguments to an operator, or nil if there are none.
	Args() []Expr

	// Equal returns true iff e1 is equal ("deep comparison") to e2.
	Equal(Expr) bool

	// Copy returns a coyp ("deep copy") of an expression.
	Copy() Expr

	fmt.Stringer
	encoding.BinaryAppender
	encoding.TextAppender
}

// leaf is any leaf type.
type leaf struct {}

func (l *leaf) isLeaf() bool {
	return true
}

func (l *leaf) IsList() bool {
	return false
}

// String is a leaf node that has text.
type String struct {
	leaf
	S string
	Hint string
}

func (s *String) Op() string {
	return s.S
}

func (s *String) Args() []Expr {
	return nil
}

func (s *String) Els() []Expr {
	return nil
}

func (s *String) Equal(e Expr) bool {
	if t, ok := e.(*String); ok {
		return s.S == t.S && s.Hint == t.Hint
	}
	return false
}

func (s *String) Copy() Expr {
	return &String{S: s.S, Hint: s.Hint}
}

func (s *String) AppendText(d []byte) ([]byte, error) {
	if s.Hint == "" && IsToken(s.S) {
		return append(d, s.S...), nil
	}
	if s.Hint != "" {
		d = append(d, '[')
		d = append(d, quote(s.Hint)...)
		d = append(d, ']')
	}
	d = append(d, quote(s.S)...)
	return d, nil
}

func (s *String) AppendBinary(d []byte) ([]byte, error) {
	return nil, nil
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

func (b *Binary) Op() string {
	return ""
}

func (b *Binary) Args() []Expr {
	return nil
}

func (b *Binary) Els() []Expr {
	return nil
}

func (b *Binary) Equal(e Expr) bool {
	if t, ok := e.(*Binary); ok {
		return bytes.Equal(b.Data, t.Data) && b.Hint == t.Hint
	}
	return false
}

func (b *Binary) Copy() Expr {
	return &Binary{Data: bytes.Clone(b.Data), Hint: b.Hint}
}

func (b *Binary) AppendText(d []byte) ([]byte, error) {
	return nil, nil
}

func (b *Binary) AppendBinary(d []byte) ([]byte, error) {
	return nil, nil
}

func (b *Binary) String() string {
	return fmt.Sprintf("%x", b.Data)
}

// List is an interior node: a list of expressions.
type List []Expr

func (l List) isLeaf() bool {
	return false
}

func (l List) IsList() bool {
	return true
}

func (l List) Op() string {
	if len(l) == 0 {
		return ""
	}
	if s, ok := l[0].(*String); ok {
		return s.S
	}
	return ""
}

func (l List) Args() []Expr {
	if len(l) == 0 {
		return nil
	}
	return l[1:]
}

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
	for _, el := range l {
		sb.WriteString(el.String())
	}
	sb.WriteByte(')')
	return sb.String()
}

func (l List) Hd() Expr {
	if len(l) == 0 {
		return nil
	}
	return l[0]
}

func (l List) Tl() List {
	if len(l) == 0 {
		return nil
	}
	return l[1:]
}

func (l List) AppendBinary(a []byte) ([]byte, error) {
	return nil, nil
}

func (l List) AppendText(a []byte) ([]byte, error) {
	return nil, nil
}

// SyntaxErr describes a syntax error, including its location in the input stream.
type SyntaxErr struct {
	Msg	string
	Offset int64
}

func (e SyntaxErr) Error() string {
	return fmt.Sprint("offset %d: %s", e.Offset, e.Msg)
}

// Reader represents a stream of S-expressions.
type Reader struct {
	rd	io.Reader
	buf	[]byte
	nb	int
	w	int
	offset	int64
	err	error
}

func (r *Reader) get() rune {
	if r.err != nil {
		return -1
	}
	for r.nb < utf8.UTFMax && r.err == nil && !utf8.FullRune(r.buf[0: r.nb]) {
		n, err := r.rd.Read(r.buf[r.nb: r.nb+1])
		if err != nil {
			r.err = err
			if n == 0 {
				return eof
			}
		}
		r.nb += n
	}
	r.offset++
	c, w := utf8.DecodeRune(r.buf[0: r.nb])
	r.w = w
	return c
}

func (r *Reader) unget() {
	if r.err != nil {
		return
	}
	r.nb = r.w
	r.offset--
}

// NewReader returns an S-expression reader for the given stream.
func NewReader(f io.Reader) *Reader {
	return &Reader{rd: f}
}

// Read returns the next S-expression from the stream, or an error.
func (rd *Reader) Read() (Expr, error) {
	e, err := rd.parseItem()
	if err != nil {
		off := err.(*SyntaxErr).Offset
		if off < 0 {
			off = rd.offset
		}
		return nil, fmt.Errorf("offset %d: %s", off, err)
	}
	return e, nil
}

// Parse parses the given string as an S-expression and returns it,
// including any trailing text, or it returns an error.
func Parse(s string) (Expr, string, error) {
	rd := NewReader(strings.NewReader(s))
	e, err := rd.Read()
	if err != nil {
		return nil, "", err
	}
	l := int(rd.offset)
	if l > len(s) {
		l = len(s)
	}
	return e, s[l:], nil
}

func (rd *Reader) parseItem() (Expr, error) {
	p0 := rd.offset
	c := rd.skipWS()
	if c < 0 {
		return nil, rd.err
	}
	switch c {
	case '{':
		a, err := rd.toClosing('}')
		if err != nil {
			return nil, err
		}
		dec, err := base64dec(a, nil)
		if err != nil {
			return nil, fmt.Errorf("base64 encoding: %w", err)
		}
		f := bytes.NewReader(dec)
		nrd := &Reader{rd: f}
		return nrd.parseItem()
	case '(':
		els := []Expr{}
		for {
			c := rd.skipWS()
			if c < 0 {
				return nil, SyntaxErr{"unclosed '('", p0}
			}
			if c == ')' {
				break
			}
			rd.unget()
			exp, err := rd.parseItem()	// we'll catch missing ) at top of loop
			if err != nil {
				continue
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
			return nil, SyntaxErr{"missing ] in display hint", p0}
		}
		if v, ok := hint.(*String); !ok {
			return nil, SyntaxErr{"illegal display hint", rd.offset}
		} else {
			return rd.simpleString(rd.skipWS(), v.S)
		}
	default:
		return rd.simpleString(c, "")
	}
}

func isSpace(c rune) bool {
	return c == ' ' || c == '\r' || c == '\t' || c == '\n'
}

// skipWS returns the first non-white-space character,
// or eof on an error including EOF.
func (rd *Reader) skipWS() rune {
	for {
		c := rd.get()
		if c < 0 || !isSpace(c) {
			return c
		}
	}
}

func (rd *Reader) simpleString(c rune, hint string) (Expr, error) {
	dec := -1
	var decs strings.Builder
	if(c >= '0' && c <= '9'){
		for dec = 0; c >= '0' && c <= '9'; c = rd.get() {
			dec = dec*10 + int(c)-'0'
			decs.WriteByte(byte(c))
		}
		if(dec < 0 || dec > MaxToken) {
			return nil, SyntaxErr{"implausible token length", rd.offset}
		}
	}
	switch c {
	case '"':
		text, err:= rd.unquote()
		if err != nil {
			return nil, err
		}
		return &String{S: text, Hint: hint}, nil
	case '|':
		dec, err := base64dec(rd.toClosing(c))
		if err != nil {
			return nil, err
		}
		return sform(dec, hint)
	case '#':
		dec, err := base16dec(rd.toClosing(c))
		if err != nil {
			return nil, err
		}
		return sform(dec, hint)
	default:
		if c == ':' && dec >= 0 {	// raw bytes
			a := make([]byte, dec)
			for i := range dec {
				c = rd.get()
				if c < 0 {
					return nil, SyntaxErr{"missing bytes in raw token", rd.offset}
				}
				a[i] = byte(c)
			}
			return sform(a, hint)
		}
		if decs.Len() != 0 {
			return nil, SyntaxErr{"token can't start with a digit", rd.offset}
		}
		var os strings.Builder 	// <token> by definition is always printable; never utf-8
		for IsTokenRune(c) {
			os.WriteRune(c)
			c = rd.get()
		}
		if os.Len() == 0 {
			return nil, SyntaxErr{"missing token", rd.offset}	// consume c to ensure progress on error
		}
		if c != eof {
			rd.unget()
		}
		return &String{S: os.String(), Hint: hint}, nil
	}
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
// the result as a string.
func (rd *Reader) toClosing(end rune) (string, error) {
	var sb strings.Builder
	p0 := rd.offset
	for {
		switch c := rd.get(); {
		case c == end:
			return sb.String(), nil
		case c < 0:
			return "", SyntaxErr{fmt.Sprintf("missing closing '%c'", end), p0}
		default:
			sb.WriteRune(c)
		}
	}
}

func hexDigit(c rune) int {
	if c >= '0' && c <= '9' {
		return int(c) - '0'
	}
	if c >= 'a' && c <= 'f' {
		return 10 + (int(c) - 'a')
	}
	if c >= 'A' && c <= 'F' {
		return 10 + (int(c) - 'A')
	}
	return -1
}

// unquote strips the quotes from the next string in the input and returns it.
// Escape sequences are also converted to the underlying character.
func (rd *Reader) unquote() (string, error) {
	var os strings.Builder
	p0 := rd.offset
	for {
		c := rd.get()
		if c == '"' {
			break
		}
		if c < 0 {
			return os.String(), SyntaxErr{"unclosed quoted string", p0}
		}
		if c == '\\' {
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
				if(c != '\r') {
					rd.unget()
				}
				continue
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			case 'v':
				c = '\v'
			case '0', '1', '2', '3', '4',
				'5', '6', '7', '8', '9':
				oct := 0
				for i := 0;; {
					if !(c >= '0' && c <= '7') {
						return os.String(), SyntaxErr{"illegal octal escape", e0}
					}
					oct = (oct<<3) | (int(c)-'0')
					if i++; i == 3 {
						break
					}
					c = rd.get()
				}
				c = rune(oct & 0xFF)
			case 'x':
				c0 := hexDigit(rd.get())
				c1 := hexDigit(rd.get())
				if c0 < 0 || c1 < 0 {
					return "", SyntaxErr{"illegal hex escape", e0}
				}
				c = rune((c0<<4) | c1)
			default:
				;	// as-is
			}
		}
		os.WriteRune(c)
	}
	return os.String(), nil
}

func hintlen(s string) int {
	n := len(s)
	if n == 0 {
		return 0	// doesn't appear at all
	}
	return len(fmt.Sprintf("[%d:]", n)) + n
}

func declen(n int) int {
	return len(fmt.Sprintf("%d:", n))
}

func packedSize(e Expr) int {
	if e == nil {
		return 0
	}
	switch r := e.(type) {
	case *String:
		n := len(r.S)
		return hintlen(r.Hint) + declen(n) + n
	case *Binary:
		n := len(r.Data)
		return hintlen(r.Hint) + declen(n) + n
	case List:
		n := 1;	// '('
		for _, el := range r {
			n += packedSize(el)
		}
		return n+1;	// + ')'
	default:
		panic("bad Expr")
	}
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

// pack appends the packed representation of e to a, returning the updated slice.
func pack(a []byte, e Expr) []byte {
	if e == nil {
		return a
	}
	switch r := e.(type) {
	case *String:
		if r.Hint != "" {
			a = packHint(a, r.Hint)
		}
		return packBytes(a, []byte(r.S))
	case *Binary:
		if r.Hint != "" {
			a = packHint(a, r.Hint)
		}
		return packBytes(a, r.Data)
	case List:
		a = append(a, '(')
		for _, el := range r {
			a = pack(a, el)
		}
		a = append(a, ')')
		return a
	default:
		panic("bad Expr")
	}
}

func Pack(e Expr) []byte {
	a := make([]byte, packedSize(e))
	a = pack(a, e)
	return a
}

func base64enc(data []byte) string {
	return base64.StdEncoding.EncodeToString(data) // TO DO: check StdEncoding
}

func base64dec(s string, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	d, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func base16enc(data []byte) string {
	return hex.EncodeToString(data)
}

func base16dec(s string, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	d, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return d, nil
}

//func (e Expr) b64text() string {
//	return "{" + base64enc(e.pack()) + "}"
//}
//
//// TO DO
//func (e Expr) String() string {
//	if e == nil {
//		return ""
//	}
//	switch r := e.(type) {
//	case *String:
//		s := quote(r.s)
//		if(r.Hint == "") {
//			return s
//		}
//		return "["+quote(r.Hint)+"]"+s
//	case *Binary:
//		h := r.Hint
//		if(h != "") {
//			h = "["+quote(h)+"]"
//		}
//		if len(r.Data) <= 8 {
//			return fmt.Sprintf("%s//%s#", h, base16enc(r.Data))
//		}
//		return fmt.Sprintf("%s|%s|", h, base64enc(r.Data))
//	case List:
//		var sb strings.Builder
//		sb.WriteByte('(')
//		for i, el := range r {
//			if i != 0 {
//				sb.WriteByte(' ')
//			}
//			sb.WriteString(el.String())
//		}
//		sb.WriteByte(')')
//		return s.String()
//	default:
//		panic("bad Expr")
//	}
//}

// IsTokenRune returns true iff rune r can appear in a token.
func IsTokenRune(r rune) bool {
	return r >= '0' && r <= '9' ||
		r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
		r == '-' || r == '.' || r == '/' || r == '_' || r == ':' || r == '*' || r == '+' || r == '='
}

// isToken enforces the following rule:
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

// isText checks whether the data should qualify as binary or text?
// the if(false) version accepts valid Unicode sequences
// could use [display] to control character set?
func isText(a []byte) bool {
	for i := range a {
		if false {
			//c, n, ok := sysbyte2char(a, i)
			//if !ok || c < ' ' && !isspace(c) || c >= 0x7F {
			//	return false
		//	}
			//i += n
		} else {
			c := rune(a[i])
			i++
			if c < ' ' && !isSpace(c) || c >= 0x7F {
				return false
			}
		}
	}
	return true
}

func esc(c byte) string {
	switch c {
	case '"':	return "\\\""
	case '\\':	return "\\\\"
	case '\b':	return "\\b"
	case '\f':	return "\\f"
	case '\n':	return "\\n"
	case '\t':	return "\\t"
	case '\r':	return "\\r"
	case '\v':	return "\\v"
	default:
		if c < ' ' || c >= 0x7F {
			return fmt.Sprint("\\x%02x", c)
		}
	}
	return ""
}

// quote returns s quoted if necessary, following the quoting rules of the RFC.
// The string is interpreted as bytes, not UTF-8, since the definition precedes
// Unicode and UTF by several years.
// We should probably have an option to select this behaviour.
func quote(s string) string {
	if IsToken(s) {
		// no quoting required
		return s
	}
	for i := range s {
		if v := esc(s[i]); v != "" {
			var os strings.Builder
			os.WriteByte('"')
			os.WriteString(s[0: i])
			os.WriteString(v)
			i++
			for i < len(s) {
				if v = esc(s[i]); v != "" {
					os.WriteString(v)
				} else {
					os.WriteByte(s[i])
				}
			}
			os.WriteByte('"')
			return os.String()
		}
	}
	return "\""+s+"\""
}

// AsData returns the value of a leaf expression as
 // data bytes. A non-leaf expression has none,
// represented as nil.
//func (e Expr) AsData() []byte {
//	if e == nil {
//		return nil
//	}
//	switch s := e.(type) {
//	case List:
//		return nil
//	case *String:
//		return []byte(s.S)
//	case *Binary:
//		return s.Data
//	}
//}

// AsText returns the value of a leaf expression as
// textual data. A non-leaf expression has none,
// represented as "".
//func (e Expr) AsText() string {
//	if e == nil {
//		return ""
//	}
//	switch s := e.(type) {
//	case List:
//		return ""
//	case String:
//		return s.S
//	case Binary:
//		return string(s.Data)
//	}
//}
