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
			subj = subj[0: i]
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
}
