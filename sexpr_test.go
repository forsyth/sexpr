package sexpr_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/forsyth/sexpr"
)

func TestSExprs(t *testing.T) {
	fd, err := os.Open("testdata/Tests")
	if err != nil {
		t.Fatalf("cannot open Tests: %s", err)
		return
	}
	defer fd.Close()
	lines := bufio.NewScanner(fd)
	for lines.Scan() {
		subj := lines.Text()
		t.Logf("<-- %s", subj)
		fails := ""
		if i := strings.Index(subj, "!ERR:"); i >= 0 {
			fails = subj[i+5:]
			subj = subj[0:i]
		}
		e, err := sexpr.Parse(subj)
		if fails != "" {
			switch {
			case err == nil:
				t.Errorf("parse %s should fail, want %q, got success", subj, fails)
			case err.Error() != fails:
				t.Errorf("parse %s should fail, want %q, got %q", subj, fails, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parse %s: error %q", subj, err)
			continue
		}
		// check canonical -> base64 -> read has same value
		b64 := sexpr.Base64(e, sexpr.Canonical)
		t.Logf("--> %s [%s]", e.String(), b64)
		x, err := sexpr.Parse(b64)
		if err != nil {
			t.Errorf("parse base64 encoding failed %s: %s", b64, err)
			continue
		}
		if !e.Equal(x) {
			t.Errorf("%s :: %s not equal after round trip", e, x)
		}
	}
	type setest struct {
		exp sexpr.Expr
		text string
	}
	s5 := sexpr.NewHintedBinary([]byte{0xF0, 0xF1}, "hint5")
	s6, err := sexpr.Parse("(nested (op (down left) up) \"top-left\")")
	if err != nil {
		t.Errorf("parse failed: expected Expr, no error; got %s", err)
	}
	setests := []setest {
		{
			sexpr.NewList(
				"watcher",
				sexpr.NewList("op", "arg1", "arg2", []byte{1, 2, 3, 4}),
				[]byte{5, 6, 7, 8},
			),
			"(watcher (op arg1 arg2 #01020304#) #05060708#)",
		},
		{
			sexpr.NewList(
				"123backagain456",
				sexpr.NewList("op", "arg1", s5, []byte{1, 2, 3, 4, 0xAF, 0xFB}),
				[]byte{0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88},
				s6,
			),
			"(\"123backagain456\" (op arg1 [hint5]#f0f1# #01020304affb#) |gIGCg4SFhoeI| (nested (op (down left) up) top-left))",
		},
	}
	for i, st := range setests {
		if s := st.exp.String();  s != st.text {
			t.Errorf("NewList test %d %#v want %s; got %s", i, st.exp, st.text, s)
		}
	}
}
