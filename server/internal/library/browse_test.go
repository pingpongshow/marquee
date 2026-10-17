package library

import (
	"os"
	"path/filepath"
	"testing"
)

// A browse root of "/" (allowed by the settings) lets the picker open any folder below it;
// a folder beside a root, sharing its name as a prefix, stays outside.
func TestContainingRoot(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	media, other := filepath.Join(dir, "media"), filepath.Join(dir, "media2")
	os.MkdirAll(filepath.Join(media, "Movies"), 0o755)
	os.MkdirAll(other, 0o755)

	if _, ok := containingRoot([]string{"/"}, filepath.Join(media, "Movies")); !ok {
		t.Error(`"/" should contain every folder`)
	}
	if _, ok := containingRoot([]string{media}, filepath.Join(media, "Movies")); !ok {
		t.Error("a subfolder of the root should be inside it")
	}
	if _, ok := containingRoot([]string{media}, other); ok {
		t.Error("a sibling folder with the root's name as a prefix should be outside")
	}
	if entries, _, err := Browse([]string{"/"}, media); err != nil || len(entries) != 1 {
		t.Errorf("browse under /: %v %v", entries, err)
	}
}
