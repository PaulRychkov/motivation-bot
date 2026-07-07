package bot

import (
	"embed"
	"io/fs"
)

//go:embed prompts/*.md
var embedded embed.FS

var PromptsFS = mustSub(embedded, "prompts")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
