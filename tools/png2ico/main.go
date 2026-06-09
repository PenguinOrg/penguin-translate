package main

import (
	"image/png"
	"os"

	ico "github.com/Kodeworks/golang-image-ico"
	xdraw "golang.org/x/image/draw"
	"image"
)

func main() {
	if len(os.Args) != 3 {
		os.Stderr.WriteString("usage: png2ico <in.png> <out.ico>\n")
		os.Exit(2)
	}
	inPath, outPath := os.Args[1], os.Args[2]
	f, err := os.Open(inPath)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		fatal(err)
	}
	const size = 256
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	out, err := os.Create(outPath)
	if err != nil {
		fatal(err)
	}
	defer out.Close()
	if err := ico.Encode(out, dst); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	os.Stderr.WriteString(err.Error() + "\n")
	os.Exit(1)
}
