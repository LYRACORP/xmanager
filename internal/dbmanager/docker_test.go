package dbmanager

import "testing"

func TestContainerFor(t *testing.T) {
	cases := map[DBType]string{
		PostgreSQL: ContainerPostgres,
		MySQL:      ContainerMySQL,
		MariaDB:    ContainerMariaDB,
		MongoDB:    ContainerMongoDB,
		Redis:      ContainerRedis,
		ClickHouse: ContainerClickHouse,
	}
	for typ, want := range cases {
		if got := containerFor(typ); got != want {
			t.Fatalf("%s: got %q want %q", typ, got, want)
		}
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
