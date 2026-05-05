package sexpr_test

import (
	"bufio"
	"os"
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
		l := lines.Text()
		t.Logf("<-- %s", l)
		e, _, err := sexpr.Parse(lines.Text())
		if err != nil {
			t.Errorf("failed %q: %s", lines.Text(), err)
			continue
		}
		b64 := sexpr.Base64(e, sexprs.Canonical)
		t.Logf("--> %s [%s]", e.String(), b64)
		x, _, err := sexpr.Parse(b64)
		if err != nil {
			t.Errorf("b64 failed %q: %s", b64, err)
			continue
		}
		if !e.Equal(x) {
			t.Errorf("%s :: %s not equal", e, x)
		}
	}
}
