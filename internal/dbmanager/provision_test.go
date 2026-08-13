package dbmanager

import "testing"

func TestSQLString(t *testing.T) {
	if got := sqlString(`a'b`); got != `a''b` {
		t.Fatalf("got %q", got)
	}
}

func TestIsSimpleIdent(t *testing.T) {
	if !isSimpleIdent("my_db") {
		t.Fatal("expected my_db simple")
	}
	if isSimpleIdent("my-db") {
		t.Fatal("hyphen should not be simple")
	}
	if isSimpleIdent("1db") {
		t.Fatal("leading digit should not be simple")
	}
}
