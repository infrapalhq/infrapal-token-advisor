package webui

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestDistFSHasIndex(t *testing.T) {
	ui, err := DistFS()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(ui, "index.html"); err != nil {
		t.Errorf("no index.html: %v", err)
	}
}

func TestChooseFS(t *testing.T) {
	built := fstest.MapFS{"index.html": {Data: []byte("built")}}
	empty := fstest.MapFS{".gitkeep": {}}
	placeholder := fstest.MapFS{"index.html": {Data: []byte("placeholder")}}

	for _, tc := range []struct {
		name string
		dist fs.FS
		want string
	}{
		{"built", built, "built"},
		{"not built", empty, "placeholder"},
	} {
		got, err := fs.ReadFile(chooseFS(tc.dist, placeholder), "index.html")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s: served %q, want %q", tc.name, got, tc.want)
		}
	}
}
