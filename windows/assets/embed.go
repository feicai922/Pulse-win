package assets

import "embed"

//go:embed pulse-mark.svg icons/*.svg
var FS embed.FS

func IconSVG(name string) ([]byte, error) {
	return FS.ReadFile("icons/" + name + ".svg")
}

func PulseMarkSVG() ([]byte, error) {
	return FS.ReadFile("pulse-mark.svg")
}
