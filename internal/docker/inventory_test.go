package docker

import "testing"

func TestParseLoginProbe(t *testing.T) {
	in := "alice\n---XM---\nauth\thttps://index.docker.io/v1/\nauth\tghcr.io\nhelper\tregistry.gitlab.com\nstore\tdesktop\n"
	got := ParseLoginProbe(in)
	if got.Username != "alice" {
		t.Fatalf("user=%q", got.Username)
	}
	if got.CredsStore != "desktop" {
		t.Fatalf("store=%q", got.CredsStore)
	}
	if got.AccountLine() != "alice @ docker.io, ghcr.io, registry.gitlab.com" {
		t.Fatalf("line=%q", got.AccountLine())
	}
	empty := ParseLoginProbe("\n---XM---\n")
	if empty.LoggedIn() {
		t.Fatal("expected not logged in")
	}
	if empty.AccountLine() != "not logged in" {
		t.Fatalf("empty line=%q", empty.AccountLine())
	}
}

func TestParseVolumeLSJSON(t *testing.T) {
	in := `{"Name":"dbdata","Driver":"local","Scope":"local"}
{"Name":"cache","Driver":"local","Scope":"local"}
`
	vols, err := ParseVolumeLSJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 2 || vols[0].Name != "dbdata" || vols[1].Driver != "local" {
		t.Fatalf("%+v", vols)
	}
}

func TestParseNetworkInspect(t *testing.T) {
	in := "bridge\tabc123def\tbridge\tlocal\tno\t2\t172.17.0.0/16\ninternal\tdef456\tbridge\tlocal\tyes\t0\t"
	nets := ParseNetworkInspect(in)
	if len(nets) != 2 {
		t.Fatalf("len=%d", len(nets))
	}
	if nets[0].Name != "bridge" || nets[0].Containers != 2 || nets[0].Subnet != "172.17.0.0/16" {
		t.Fatalf("%+v", nets[0])
	}
	if !nets[1].Internal {
		t.Fatal("expected internal")
	}
}

func TestParseVerboseDF(t *testing.T) {
	in := `Images space usage:

REPOSITORY   TAG       IMAGE ID       CREATED        SIZE      SHARED SIZE   UNIQUE SIZE   CONTAINERS
nginx        latest    e799c7f9cae7   5 days ago     187MB     0B            187MB         2

Containers space usage:

CONTAINER ID   IMAGE     COMMAND   LOCAL VOLUMES   SIZE      CREATED          STATUS     NAMES
4ee19bba0235   nginx     "httpd"   0               1.09kB    16 seconds ago   Up         web

Local Volumes space usage:

VOLUME NAME                     LINKS     SIZE
my-vol                          1         27.32MB
project_db                      1         84.19MB
dangling-abc                    0         0B

Build cache usage: 1.234GB

CACHE ID       CACHE TYPE       SIZE      CREATED               LAST USED             USAGE     SHARED
b63xm0f8jahi   regular          181.9MB   About an hour ago     2 minutes ago         1         false
cafebabe1234   source.local     12.5MB    4 days ago            4 days ago            3         true
`
	df := ParseVerboseDF(in)
	if len(df.Volumes) != 3 {
		t.Fatalf("vols=%d %+v", len(df.Volumes), df.Volumes)
	}
	if df.Volumes[0].Name != "my-vol" || df.Volumes[0].Links != 1 {
		t.Fatalf("%+v", df.Volumes[0])
	}
	if df.Volumes[0].SizeBytes != ParseDockerSize("27.32MB") {
		t.Fatalf("size=%d", df.Volumes[0].SizeBytes)
	}
	if len(df.Cache) != 2 {
		t.Fatalf("cache=%d %+v", len(df.Cache), df.Cache)
	}
	if df.Cache[0].ID != "b63xm0f8jahi" || df.Cache[0].Type != "regular" || df.Cache[0].Shared {
		t.Fatalf("%+v", df.Cache[0])
	}
	if df.Cache[1].Type != "source.local" || !df.Cache[1].Shared || df.Cache[1].Usage != 3 {
		t.Fatalf("%+v", df.Cache[1])
	}
}

func TestParseBuildxDU(t *testing.T) {
	in := `ID                              RECLAIMABLE     SIZE        LAST ACCESSED
abcd1234*                       true            181.9MB     2 minutes ago
deadbeef                        false           1.2GB       4 days ago
Shared:		12.4MB
Private:		1.24GB
Reclaimable:		1.25GB
Total:			1.25GB
`
	got := ParseBuildxDU(in)
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].ID != "abcd1234" || !got[0].Reclaimable {
		t.Fatalf("%+v", got[0])
	}
	if got[1].SizeBytes != ParseDockerSize("1.2GB") {
		t.Fatalf("%+v", got[1])
	}
}

func TestMergeVolumes(t *testing.T) {
	meta := []Volume{{Name: "dbdata", Driver: "local", Scope: "local"}, {Name: "empty", Driver: "local"}}
	sized := []Volume{{Name: "dbdata", Links: 2, SizeHuman: "80MB", SizeBytes: ParseDockerSize("80MB")}}
	got := mergeVolumes(meta, sized)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	byName := map[string]Volume{}
	for _, v := range got {
		byName[v.Name] = v
	}
	if byName["dbdata"].Driver != "local" || byName["dbdata"].Links != 2 || byName["dbdata"].SizeHuman != "80MB" {
		t.Fatalf("%+v", byName["dbdata"])
	}
	if byName["empty"].SizeHuman != "—" {
		t.Fatalf("%+v", byName["empty"])
	}
}

func TestPrettyRegistry(t *testing.T) {
	if prettyRegistry("https://index.docker.io/v1/") != "docker.io" {
		t.Fatal(prettyRegistry("https://index.docker.io/v1/"))
	}
	if prettyRegistry("ghcr.io") != "ghcr.io" {
		t.Fatal(prettyRegistry("ghcr.io"))
	}
}

func TestFormatSize(t *testing.T) {
	if FormatSize(512) != "512B" {
		t.Fatal(FormatSize(512))
	}
	if FormatSize(1024) != "1.0KB" {
		t.Fatal(FormatSize(1024))
	}
}
