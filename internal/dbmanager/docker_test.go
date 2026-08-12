package dbmanager

import "testing"

func TestContainerName(t *testing.T) {
	cases := map[DBType]string{
		PostgreSQL: ContainerPostgres,
		MySQL:      ContainerMySQL,
		MariaDB:    ContainerMariaDB,
		MongoDB:    ContainerMongoDB,
		Redis:      ContainerRedis,
		ClickHouse: ContainerClickHouse,
	}
	for typ, want := range cases {
		if got := ContainerName(typ); got != want {
			t.Fatalf("%s: got %q want %q", typ, got, want)
		}
	}
}

func TestParseHostPort(t *testing.T) {
	if got := ParseHostPort("0.0.0.0:5432->5432/tcp"); got != "5432" {
		t.Fatalf("got %q", got)
	}
	if got := ParseHostPort(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("abc"); got != `'abc'` {
		t.Fatalf("got %q", got)
	}
	if got := shellQuote(`a'b`); got != `'a'\''b'` {
		t.Fatalf("got %q", got)
	}
}
